// Disposable controller inside the existing dedicated Docker daemon. TCP
// forwarding lets Chromium reach it without exposing a daemon port or keys.
import { execFile, spawn } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdtemp, rm, cp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { createServer } from 'node:net';

const exec = promisify(execFile);
const repo = resolve(import.meta.dirname, '..');
const daemon = process.env.WEFT_TEST_SANDBOX_CONTAINER || 'weft-sandbox-daemon-1';
const docker = async (...args) => (await exec('docker', args, { maxBuffer: 4 * 1024 * 1024 })).stdout.trim();
const service = await docker('inspect', '--format', '{{index .Config.Labels "com.docker.compose.service"}}', daemon);
if (service !== 'sandbox-daemon') throw new Error('Expected the dedicated sandbox-daemon service');
await docker('exec', daemon, 'docker', 'image', 'inspect', 'alpine:3.22');
const arch = await docker('exec', daemon, 'uname', '-m');
const goarch = { aarch64: 'arm64', x86_64: 'amd64' }[arch];
if (!goarch) throw new Error(`Unsupported daemon architecture: ${arch}`);
const local = await mkdtemp(join(tmpdir(), 'weft-browser-real-'));
let remote, controller, server, previewServer;
const forwards = new Set();
let stopping = false;
async function shutdown() {
  if (stopping) return;
  stopping = true;
  try {
    server?.close();
    previewServer?.close();
    for (const socket of forwards) socket.destroy();
    if (controller && controller.exitCode === null) {
      const pid = await docker('exec', daemon, 'cat', `${remote}/pid`);
      if (!/^\d+$/.test(pid)) throw new Error('Invalid fixture controller PID');
      await docker('exec', daemon, 'kill', '-TERM', pid);
      const code = await new Promise(resolve => {
        controller.once('exit', resolve);
        if (controller.exitCode !== null) resolve(controller.exitCode);
      });
      if (code !== 0) throw new Error(`Fixture cleanup failed (${code}); retained ${remote} for inspection`);
    }
    if (controller && controller.exitCode !== 0) throw new Error(`Controller failed; retained ${remote} for inspection`);
    if (remote) await docker('exec', daemon, 'rm', '-rf', remote);
  } catch (error) { console.error(error); process.exitCode = 1; }
  finally { await rm(local, { recursive: true, force: true }); }
}
process.once('SIGTERM', shutdown);
process.once('SIGINT', shutdown);
try {
  await exec('go', ['build', '-tags', 'e2e', '-o', join(local, 'controller'), './cmd/weft-e2e'], {
    cwd: join(repo, 'server'), env: { ...process.env, GOOS: 'linux', GOARCH: goarch, CGO_ENABLED: '0' },
  });
  await exec('go', ['build', '-o', join(local, 'weft-preview-linux'), './cmd/weft-preview'], {cwd:join(repo,'server'),env:{...process.env,GOOS:'linux',GOARCH:goarch,CGO_ENABLED:'0'}});
  await exec('pnpm',['exec','vite','build','preview-fixture','--config','vite.config.ts','--outDir','../preview-fixture-dist'],{cwd:join(repo,'web')});
  await cp(join(repo,'web/preview-fixture-dist'),join(local,'preview-web'),{recursive:true});
  const candidate = await docker('exec', daemon, 'mktemp', '-d', '/tmp/weft-browser-real.XXXXXX');
  if (!/^\/tmp\/weft-browser-real\.[A-Za-z0-9]+$/.test(candidate)) throw new Error('Unexpected temporary path');
  remote = candidate;
  await cp(join(repo, 'examples/projects/mise-node'), join(local, 'mise-node'), { recursive: true });
  await cp(join(repo, 'web/dist'), join(local, 'web'), { recursive: true });
  await new Promise((resolve, reject) => {
    const archive = spawn('tar', ['-C', local, '-cf', '-', 'controller', 'web', 'mise-node', 'weft-preview-linux', 'preview-web']);
    const unpack = spawn('docker', ['exec', '-i', daemon, 'tar', '-C', remote, '-xf', '-']);
    archive.stdout.pipe(unpack.stdin);
    archive.stderr.pipe(process.stderr); unpack.stderr.pipe(process.stderr);
    archive.on('error', reject); unpack.on('error', reject); unpack.stdin.on('error', reject);
    const completed = child => new Promise((done, fail) => child.once('exit', code => code === 0 ? done() : fail(new Error(`Fixture transfer failed (${code})`))));
    Promise.all([completed(archive), completed(unpack)]).then(resolve, reject);
  });
  controller = spawn('docker', ['exec', '-w', remote, '-e', 'WEFT_E2E_REAL=1', daemon, './controller'], { detached: true, stdio: ['ignore', 'inherit', 'inherit'] });
  controller.once('error', error => { console.error(error); process.exitCode = 1; void shutdown(); });
  controller.once('exit', code => {
    if (!stopping) { console.error(`Fixture controller exited unexpectedly (${code})`); process.exitCode = 1; void shutdown(); }
  });
  server = createServer(socket => {
    forwards.add(socket);
    const tunnel = spawn('docker', ['exec', '-i', daemon, 'nc', '127.0.0.1', '18090']);
    socket.pipe(tunnel.stdin);
    tunnel.stdout.pipe(socket);
    tunnel.stderr.resume();
    tunnel.stdin.on('error', () => socket.destroy());
    tunnel.once('error', error => socket.destroy(error));
    tunnel.once('exit', () => socket.destroy());
    socket.on('error', () => {}); // connection failures are surfaced to the HTTP client
    socket.once('close', () => { forwards.delete(socket); tunnel.kill(); });
  });
  server.on('error', error => { console.error(error); process.exitCode = 1; void shutdown(); });
  server.listen(18090, '127.0.0.1');
  previewServer = createServer(socket => {
    forwards.add(socket);
    const tunnel = spawn('docker',['exec','-i',daemon,'nc','127.0.0.1','18092']);
    socket.pipe(tunnel.stdin);tunnel.stdout.pipe(socket);tunnel.stderr.resume();
    tunnel.stdin.on('error',()=>socket.destroy());tunnel.once('error',error=>socket.destroy(error));tunnel.once('exit',()=>socket.destroy());socket.on('error',()=>{});socket.once('close',()=>{forwards.delete(socket);tunnel.kill();});
  });
  previewServer.on('error',error=>{console.error(error);process.exitCode=1;void shutdown();});
  previewServer.listen(18092,'127.0.0.1');
} catch (error) { console.error(error); process.exitCode = 1; await shutdown(); }
