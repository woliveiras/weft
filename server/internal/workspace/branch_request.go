package workspace

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/woliveiras/weft/server/internal/domain"
)

type BranchPublication struct {
	Commit   string `json:"commit"`
	Previous string `json:"previous"`
}

type PublishedBranch struct {
	Branch string `json:"branch"`
	Commit string `json:"commit"`
}

// Both entry points require an explicit previous value, including for creation.
func DecodeBranchPublication(r io.Reader) (BranchPublication, error) {
	var in struct {
		Commit   string  `json:"commit"`
		Previous *string `json:"previous"`
	}
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || dec.Decode(new(any)) != io.EOF || in.Previous == nil {
		return BranchPublication{}, errors.New("expected one object with commit and previous fields")
	}
	if !domain.ValidFullCommit(in.Commit) || *in.Previous != "" && (!domain.ValidFullCommit(*in.Previous) || len(*in.Previous) != len(in.Commit)) {
		return BranchPublication{}, errors.New("supply full commit IDs and use an empty previous value for branch creation")
	}
	return BranchPublication{Commit: in.Commit, Previous: *in.Previous}, nil
}
