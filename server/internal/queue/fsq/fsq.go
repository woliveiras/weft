// Package fsq is a filesystem broker: each channel is stored in its own folder
// model, hardened with leases, retries and dead letters.
//
// # Layout
//
//	<root>/<subject>/groups.json             declared groups + delivery config
//	<root>/<subject>/.lock/                  mkdir-based lock for declare/delete
//	<root>/<subject>/<group>/tmp/            in-progress writes (never read)
//	<root>/<subject>/<group>/ready/          <notBefore>-<seq>-<attempts>-<rand>.json
//	<root>/<subject>/<group>/inflight/       <leaseUntil>-<seq>-<attempts>-<rand>.json
//	<root>/<subject>/<group>/dead/           <seq>-<attempts>-<rand>.json (+ .reason)
//
// Every state change is a single rename(2), which is atomic within one
// filesystem. The file NAME carries all mutable state - the time it becomes
// visible, the lease expiry and the delivery count - so no file is ever
// rewritten and no lock is needed on the hot path:
//
//   - publish writes a copy per group into tmp/ and renames it into ready/,
//     so a consumer never sees a half-written item;
//   - claiming renames ready/X to inflight/<lease>-...; of two racing
//     consumers (threads or processes) exactly one rename succeeds;
//   - Extend renames inflight to a later lease; Ack deletes; Nack renames back
//     to ready/ with a future notBefore; Term renames to dead/;
//   - any consumer may reap an inflight file whose lease (in its name) has
//     passed, by renaming it back to ready/. If the holder races the reaper,
//     one rename wins and the loser sees ENOENT ("lease lost").
//
// # Trade-offs
//
// Zero dependencies, human-inspectable (ls is a queue browser), and safe for
// several processes on one host or a POSIX shared filesystem with atomic
// rename. Not suited to high throughput (one fsync-less file per message and
// group, directory scans on fetch) nor to NFS variants without atomic
// rename. Fetch polls with back-off (50ms -> 500ms) rather than using inotify,
// which keeps it portable to Linux, macOS and Windows. Clock skew between
// processes sharing a directory shifts lease expiry by the skew.
package fsq

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/queue/internal/qutil"
)

// ErrLeaseLost is returned when a delivery was reaped (lease expired) or
// already settled before the operation.
var ErrLeaseLost = errors.New("fsq: delivery lease lost")

type Broker struct {
	root   string
	mu     sync.Mutex
	seq    int64
	closed bool
	done   chan struct{}
}

// New opens (and creates) a filesystem broker rooted at dir.
func New(dir string) (*Broker, error) {
	if dir == "" {
		return nil, errors.New("fsq: root directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("fsq: %w", err)
	}
	return &Broker{root: dir, done: make(chan struct{})}, nil
}

func (b *Broker) Name() string { return "fs" }

type groupsFile struct {
	Groups       []string `json:"groups"`
	MaxDeliver   int      `json:"maxDeliver"`
	AckWaitMs    int64    `json:"ackWaitMs"`
	RetentionSec int64    `json:"retentionSec"`
}

func (b *Broker) subjDir(s string) string     { return filepath.Join(b.root, s) }
func (b *Broker) groupDir(s, g string) string { return filepath.Join(b.root, s, g) }
func (b *Broker) groupsPath(s string) string  { return filepath.Join(b.root, s, "groups.json") }

func (b *Broker) readGroups(subject string) (*groupsFile, error) {
	raw, err := os.ReadFile(b.groupsPath(subject))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var gf groupsFile
	if err := json.Unmarshal(raw, &gf); err != nil {
		return nil, fmt.Errorf("fsq: corrupt %s: %w", b.groupsPath(subject), err)
	}
	return &gf, nil
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp-" + randHex(4)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// lock takes a cross-process lock on a subject via mkdir. A lock older than
// 30s is considered abandoned (crashed holder) and broken.
func (b *Broker) lock(ctx context.Context, subject string) (func(), error) {
	dir := filepath.Join(b.subjDir(subject), ".lock")
	if err := os.MkdirAll(b.subjDir(subject), 0o755); err != nil {
		return nil, err
	}
	for {
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return func() { _ = os.Remove(dir) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if fi, serr := os.Stat(dir); serr == nil && time.Since(fi.ModTime()) > 30*time.Second {
			_ = os.Remove(dir)
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (b *Broker) Declare(ctx context.Context, cfg queue.SubjectConfig) error {
	if b.isClosed() {
		return queue.ErrClosed
	}
	if err := qutil.ValidName("subject", cfg.Subject); err != nil {
		return err
	}
	for _, g := range cfg.Groups {
		if err := qutil.ValidName("group", g); err != nil {
			return err
		}
		if g == "groups.json" {
			return fmt.Errorf("fsq: reserved group name %q", g)
		}
	}
	cfg = cfg.WithDefaults()
	unlock, err := b.lock(ctx, cfg.Subject)
	if err != nil {
		return err
	}
	defer unlock()
	gf, err := b.readGroups(cfg.Subject)
	if err != nil {
		return err
	}
	if gf == nil {
		gf = &groupsFile{}
	}
	for _, g := range cfg.Groups {
		for _, d := range []string{"tmp", "ready", "inflight", "dead"} {
			if err := os.MkdirAll(filepath.Join(b.groupDir(cfg.Subject, g), d), 0o755); err != nil {
				return err
			}
		}
		if !containsStr(gf.Groups, g) {
			gf.Groups = append(gf.Groups, g)
		}
	}
	sort.Strings(gf.Groups)
	gf.MaxDeliver = cfg.MaxDeliver
	gf.AckWaitMs = cfg.AckWait.Milliseconds()
	gf.RetentionSec = int64(cfg.Retention.Seconds())
	raw, _ := json.MarshalIndent(gf, "", "  ")
	return writeAtomic(b.groupsPath(cfg.Subject), raw)
}

func (b *Broker) DeleteGroup(ctx context.Context, subject, group string) error {
	if err := qutil.ValidName("group", group); err != nil {
		return err
	}
	unlock, err := b.lock(ctx, subject)
	if err != nil {
		return err
	}
	defer unlock()
	gf, err := b.readGroups(subject)
	if err != nil || gf == nil {
		return err
	}
	out := gf.Groups[:0]
	for _, g := range gf.Groups {
		if g != group {
			out = append(out, g)
		}
	}
	gf.Groups = out
	raw, _ := json.MarshalIndent(gf, "", "  ")
	if err := writeAtomic(b.groupsPath(subject), raw); err != nil {
		return err
	}
	return os.RemoveAll(b.groupDir(subject, group))
}

func (b *Broker) nextSeq() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now().UnixNano()
	if now <= b.seq {
		now = b.seq + 1
	}
	b.seq = now
	return now
}

func (b *Broker) Publish(ctx context.Context, subject string, msg *domain.Message) error {
	if b.isClosed() {
		return queue.ErrClosed
	}
	if err := qutil.ValidName("subject", subject); err != nil {
		return err
	}
	data, err := qutil.Encode(msg)
	if err != nil {
		return err
	}
	gf, err := b.readGroups(subject)
	if err != nil {
		return err
	}
	if gf == nil || len(gf.Groups) == 0 {
		return queue.ErrNoGroups
	}
	seq := b.nextSeq()
	r := randHex(4)
	name := readyName(seq, seq, 0, r)
	for _, g := range gf.Groups {
		gd := b.groupDir(subject, g)
		tmp := filepath.Join(gd, "tmp", name)
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			if os.IsNotExist(err) {
				// Group directory removed concurrently (DeleteGroup); skip it.
				continue
			}
			return fmt.Errorf("fsq: publish: %w", err)
		}
		if err := os.Rename(tmp, filepath.Join(gd, "ready", name)); err != nil {
			return fmt.Errorf("fsq: publish: %w", err)
		}
	}
	return nil
}

func (b *Broker) Subscribe(ctx context.Context, subject, group, consumer string) (queue.Subscription, error) {
	if b.isClosed() {
		return nil, queue.ErrClosed
	}
	gf, err := b.readGroups(subject)
	if err != nil {
		return nil, err
	}
	if gf == nil || !containsStr(gf.Groups, group) {
		return nil, fmt.Errorf("fsq: group %s not declared on %s", group, subject)
	}
	return &sub{b: b, subject: subject, group: group, dir: b.groupDir(subject, group), done: make(chan struct{})}, nil
}

func (b *Broker) Stats(ctx context.Context, subject, group string) (queue.Stats, error) {
	gd := b.groupDir(subject, group)
	count := func(d string) int64 {
		ents, err := os.ReadDir(filepath.Join(gd, d))
		if err != nil {
			return 0
		}
		var n int64
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".json") {
				n++
			}
		}
		return n
	}
	// Expired leases are pending work, not in-flight work.
	if gf, err := b.readGroups(subject); err == nil && gf != nil {
		reap(gd, time.Now())
	}
	return queue.Stats{Pending: count("ready"), InFlight: count("inflight"), Dead: count("dead")}, nil
}

func (b *Broker) Ping(ctx context.Context) error {
	if b.isClosed() {
		return queue.ErrClosed
	}
	_, err := os.Stat(b.root)
	return err
}

func (b *Broker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		close(b.done)
	}
	return nil
}

func (b *Broker) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// ---- file names -----------------------------------------------------------

// ready:    <notBefore>-<seq>-<attempts>-<rand>.json
// inflight: <leaseUntil>-<seq>-<attempts>-<rand>.json
// dead:     <seq>-<attempts>-<rand>.json
type fname struct {
	t        int64 // notBefore or leaseUntil (unix nanos)
	seq      int64
	attempts int
	rand     string
}

func readyName(t, seq int64, attempts int, r string) string {
	return fmt.Sprintf("%020d-%020d-%04d-%s.json", t, seq, attempts, r)
}

func (f fname) String() string { return readyName(f.t, f.seq, f.attempts, f.rand) }
func (f fname) deadName() string {
	return fmt.Sprintf("%020d-%04d-%s.json", f.seq, f.attempts, f.rand)
}

func parseName(n string) (fname, bool) {
	if !strings.HasSuffix(n, ".json") {
		return fname{}, false
	}
	parts := strings.Split(strings.TrimSuffix(n, ".json"), "-")
	if len(parts) != 4 {
		return fname{}, false
	}
	t, err1 := strconv.ParseInt(parts[0], 10, 64)
	s, err2 := strconv.ParseInt(parts[1], 10, 64)
	a, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return fname{}, false
	}
	return fname{t: t, seq: s, attempts: a, rand: parts[3]}, true
}

// reap renames expired inflight files back to ready. Safe to run from any
// number of processes concurrently.
func reap(gd string, now time.Time) {
	ents, err := os.ReadDir(filepath.Join(gd, "inflight"))
	if err != nil {
		return
	}
	for _, e := range ents {
		f, ok := parseName(e.Name())
		if !ok || f.t > now.UnixNano() {
			continue
		}
		nf := f
		nf.t = now.UnixNano()
		_ = os.Rename(filepath.Join(gd, "inflight", e.Name()), filepath.Join(gd, "ready", nf.String()))
	}
}

// ---- subscription -----------------------------------------------------------

type sub struct {
	b       *Broker
	subject string
	group   string
	dir     string
	once    sync.Once
	done    chan struct{}
	lastCfg time.Time
	cfg     groupsFile
}

func (s *sub) config() groupsFile {
	if time.Since(s.lastCfg) > time.Second {
		if gf, err := s.b.readGroups(s.subject); err == nil && gf != nil {
			s.cfg = *gf
		}
		s.lastCfg = time.Now()
	}
	c := s.cfg
	if c.MaxDeliver <= 0 {
		c.MaxDeliver = 5
	}
	if c.AckWaitMs <= 0 {
		c.AckWaitMs = (10 * time.Minute).Milliseconds()
	}
	return c
}

func (s *sub) Fetch(ctx context.Context, max int, wait time.Duration) ([]queue.Delivery, error) {
	if max <= 0 {
		max = 1
	}
	deadline := time.Now().Add(wait)
	bo := qutil.Backoff{Min: 50 * time.Millisecond, Max: 500 * time.Millisecond}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		select {
		case <-s.done:
			return nil, queue.ErrClosed
		case <-s.b.done:
			return nil, queue.ErrClosed
		default:
		}
		out, err := s.take(max)
		if err != nil || len(out) > 0 {
			return out, err
		}
		now := time.Now()
		if !now.Before(deadline) {
			return nil, nil
		}
		d := bo.Next()
		if r := deadline.Sub(now); r < d {
			d = r
		}
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-s.done:
			t.Stop()
			return nil, queue.ErrClosed
		case <-t.C:
		}
	}
}

func (s *sub) take(max int) ([]queue.Delivery, error) {
	cfg := s.config()
	now := time.Now()
	reap(s.dir, now)
	ents, err := os.ReadDir(filepath.Join(s.dir, "ready"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("fsq: group deleted")
		}
		return nil, err
	}
	// ReadDir sorts by name; the name starts with notBefore then seq, so this
	// is due-time order, which equals publish order for un-delayed items.
	var out []queue.Delivery
	for _, e := range ents {
		if len(out) >= max {
			break
		}
		f, ok := parseName(e.Name())
		if !ok {
			continue
		}
		if f.t > now.UnixNano() {
			break // everything after this is due later
		}
		src := filepath.Join(s.dir, "ready", e.Name())
		if f.attempts >= cfg.MaxDeliver {
			if os.Rename(src, filepath.Join(s.dir, "dead", f.deadName())) == nil {
				_ = os.WriteFile(filepath.Join(s.dir, "dead", f.deadName()+".reason"), []byte("max deliveries exceeded"), 0o644)
			}
			continue
		}
		nf := f
		nf.attempts++
		nf.t = now.Add(time.Duration(cfg.AckWaitMs) * time.Millisecond).UnixNano()
		dst := filepath.Join(s.dir, "inflight", nf.String())
		if err := os.Rename(src, dst); err != nil {
			continue // another consumer won the race
		}
		data, err := os.ReadFile(dst)
		if err != nil {
			continue
		}
		m, err := qutil.Decode(data, nf.attempts)
		if err != nil {
			// Poison file: dead-letter immediately rather than looping on it.
			_ = os.Rename(dst, filepath.Join(s.dir, "dead", nf.deadName()))
			_ = os.WriteFile(filepath.Join(s.dir, "dead", nf.deadName()+".reason"), []byte(err.Error()), 0o644)
			continue
		}
		out = append(out, &delivery{s: s, cur: nf, msg: m, ackWait: time.Duration(cfg.AckWaitMs) * time.Millisecond, maxDeliver: cfg.MaxDeliver})
	}
	return out, nil
}

func (s *sub) Close() error {
	s.once.Do(func() { close(s.done) })
	return nil
}

type delivery struct {
	s          *sub
	mu         sync.Mutex
	cur        fname
	msg        *domain.Message
	ackWait    time.Duration
	maxDeliver int
	settled    bool
}

func (d *delivery) Message() *domain.Message { return d.msg }
func (d *delivery) Attempt() int             { return d.cur.attempts }

func (d *delivery) path() string { return filepath.Join(d.s.dir, "inflight", d.cur.String()) }

func (d *delivery) move(dst string) error {
	if d.settled {
		return ErrLeaseLost
	}
	if err := os.Rename(d.path(), dst); err != nil {
		if os.IsNotExist(err) {
			return ErrLeaseLost
		}
		return err
	}
	return nil
}

func (d *delivery) Ack(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.settled {
		return ErrLeaseLost
	}
	if err := os.Remove(d.path()); err != nil {
		if os.IsNotExist(err) {
			return ErrLeaseLost
		}
		return err
	}
	d.settled = true
	return nil
}

func (d *delivery) Nack(ctx context.Context, delay time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cur.attempts >= d.maxDeliver {
		return d.termLocked("max deliveries exceeded")
	}
	nf := d.cur
	nf.t = time.Now().Add(delay).UnixNano()
	if err := d.move(filepath.Join(d.s.dir, "ready", nf.String())); err != nil {
		return err
	}
	d.settled = true
	return nil
}

func (d *delivery) Term(ctx context.Context, reason string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.termLocked(reason)
}

func (d *delivery) termLocked(reason string) error {
	dead := filepath.Join(d.s.dir, "dead", d.cur.deadName())
	if err := d.move(dead); err != nil {
		return err
	}
	d.settled = true
	if reason != "" {
		_ = os.WriteFile(dead+".reason", []byte(reason), 0o644)
	}
	return nil
}

func (d *delivery) Extend(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	nf := d.cur
	nf.t = time.Now().Add(d.ackWait).UnixNano()
	if err := d.move(filepath.Join(d.s.dir, "inflight", nf.String())); err != nil {
		return err
	}
	d.cur = nf
	return nil
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var _ queue.Broker = (*Broker)(nil)
