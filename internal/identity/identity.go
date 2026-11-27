// Package identity derives the content-addressed hash12 identities and tags
// of the toolchain-sets world (spec "toolchain sets" §6, ticket #194): hash12
// over sorted (toolchain id, recipe content hash) pairs plus the base recipe
// content hash. It is pinned to the bash implementation
// (dist/scripts/lib-identity.sh, sourced by build-images.sh and start.sh) by
// the golden vectors in tests/fixtures/identity-golden — both suites
// (this package's tests and tests/test-identity-golden.sh) must stay green
// together.
//
// Serialization (pinned by the golden vectors, not prose):
//
//	content hash     = sha256 hex of the file's exact bytes
//	base hash12      = first 12 hex of sha256(base.Dockerfile bytes)
//	identity input   = "base <base content hash>" line, then one
//	                   "tc <id> <recipe content hash>" line per member,
//	                   members sorted by id (byte order), \n-terminated lines
//	hash12           = first 12 lowercase hex of sha256(identity input)
//	component hash12 = the identity input with exactly one member
//	                   (recipe content hash + base recipe content hash)
//	tags             = opencode-sandbox-base:<hash12> /
//	                   opencode-sandbox-tc-<id>:<hash12> /
//	                   opencode-sandbox-set:<hash12>
package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
)

// Repos (image names, no tag) of the family — spec §6.
const (
	BaseRepo      = "opencode-sandbox-base"
	ComponentRepo = "opencode-sandbox-tc-"
	SetRepo       = "opencode-sandbox-set"
)

// Member is one set member: a toolchain id and its recipe file. ID must be a
// valid recipe id ([a-z0-9-], "base" reserved — validated at discovery, not
// here).
type Member struct {
	ID         string
	RecipePath string
}

// ContentHash returns the sha256 hex of the file's exact bytes.
func ContentHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// BaseHash12 returns the first 12 hex of sha256 over the base recipe's bytes.
func BaseHash12(basePath string) (string, error) {
	baseHash, err := ContentHash(basePath)
	if err != nil {
		return "", err
	}
	return baseHash[:12], nil
}

// BaseTag returns opencode-sandbox-base:<hash12>.
func BaseTag(basePath string) (string, error) {
	h12, err := BaseHash12(basePath)
	if err != nil {
		return "", err
	}
	return BaseRepo + ":" + h12, nil
}

// ComponentHash12 returns the hash12 of one toolchain: the identity input of
// a single-member set — recipe content hash + base recipe content hash, so a
// base bump re-addresses every component.
func ComponentHash12(basePath, id, recipePath string) (string, error) {
	return setHash12(basePath, []Member{{ID: id, RecipePath: recipePath}})
}

// ComponentTag returns opencode-sandbox-tc-<id>:<hash12>.
func ComponentTag(basePath, id, recipePath string) (string, error) {
	h12, err := ComponentHash12(basePath, id, recipePath)
	if err != nil {
		return "", err
	}
	return ComponentRepo + id + ":" + h12, nil
}

// SetHash12 returns the set's hash12 over the given members. Member order is
// irrelevant — members are sorted by id before hashing. The empty member set
// (base-only sets) hashes the base line alone.
func SetHash12(basePath string, members []Member) (string, error) {
	return setHash12(basePath, members)
}

// SetTag returns opencode-sandbox-set:<hash12>.
func SetTag(basePath string, members []Member) (string, error) {
	h12, err := SetHash12(basePath, members)
	if err != nil {
		return "", err
	}
	return SetRepo + ":" + h12, nil
}

func setHash12(basePath string, members []Member) (string, error) {
	input, err := identityInput(basePath, members)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:])[:12], nil
}

// identityInput builds the pinned serialization: the "base" line, then one
// "tc <id> <recipe content hash>" line per member, sorted by id.
func identityInput(basePath string, members []Member) ([]byte, error) {
	baseHash, err := ContentHash(basePath)
	if err != nil {
		return nil, fmt.Errorf("base recipe: %w", err)
	}
	pairs := make([]Member, len(members))
	copy(pairs, members)
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].ID < pairs[j].ID })
	input := []byte("base " + baseHash + "\n")
	for _, m := range pairs {
		recipeHash, err := ContentHash(m.RecipePath)
		if err != nil {
			return nil, fmt.Errorf("recipe %s: %w", m.ID, err)
		}
		input = append(input, []byte("tc "+m.ID+" "+recipeHash+"\n")...)
	}
	return input, nil
}
