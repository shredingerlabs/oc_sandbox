package webserver

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// portFixture is a legacy Dockerfile.custom in the shipped template's
// shape: an all-commented example (inert), one live stage with the
// template's run-of-the-mill lines (continuations, ARG/WORKDIR/
// ENTRYPOINT strays) and a second one-line stage after an unrelated
// helper stage.
const portFixture = `# =============================================================================
# Custom edition Dockerfile — Vorlage für eigene Editionen
# =============================================================================
# Alle Zeilen des Beispiels sind auskommentiert. Zum Aktivieren die führenden
# '# ' entfernen und den Namen anpassen.
#
# FROM opencode-sandbox-rust

FROM opencode-sandbox-base AS opencode-sandbox-rust

USER root

# --- System packages (rust toolchain) ---------------------------------------
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential curl file git \
    && rm -rf /var/lib/apt/lists/*

# --- Rust / Cargo -------------------------------------------------------------
ARG RUSTUP_HOME=/usr/local/rustup
ENV RUSTUP_HOME=${RUSTUP_HOME} \
    CARGO_HOME=/usr/local/cargo
RUN curl -fsSL https://sh.rustup.rs | sh -s -- -y --default-toolchain stable

# --- Non-root user (Pflicht, wie in jeder Edition) ----------------------------
USER dev
WORKDIR /home/dev/workspace

ENTRYPOINT ["/bin/bash", "-l"]

FROM ubuntu:24.04 AS helper

FROM opencode-sandbox-base AS opencode-sandbox-notes
# USER root
RUN pip3 install --break-system-packages --no-cache-dir notes==1.0.0
USER dev
`

// The porting gist parses a legacy Dockerfile.custom: every uncommented
// stage named opencode-sandbox-<id> yields one portStage with exactly
// its RUN/ENV/USER lines (backslash continuations joined, comments and
// ARG/WORKDIR/ENTRYPOINT strays out — the recipe grammar's input, §2).
func TestCustomPortingParsesUncommentedStages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Dockerfile.custom")
	if err := os.WriteFile(path, []byte(portFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	stages := customPorting(path)
	if len(stages) != 2 {
		t.Fatalf("stages = %d (%v), want 2", len(stages), stages)
	}
	if stages[0].ID != "rust" || stages[1].ID != "notes" {
		t.Fatalf("ids = %q, %q; want rust, notes", stages[0].ID, stages[1].ID)
	}
	wantRust := []string{
		"USER root",
		"RUN apt-get update && apt-get install -y --no-install-recommends build-essential curl file git && rm -rf /var/lib/apt/lists/*",
		"ENV RUSTUP_HOME=${RUSTUP_HOME} CARGO_HOME=/usr/local/cargo",
		"RUN curl -fsSL https://sh.rustup.rs | sh -s -- -y --default-toolchain stable",
		"USER dev",
	}
	if !slices.Equal(stages[0].Lines, wantRust) {
		t.Fatalf("rust lines = %q, want %q", stages[0].Lines, wantRust)
	}
	wantNotes := []string{
		"RUN pip3 install --break-system-packages --no-cache-dir notes==1.0.0",
		"USER dev",
	}
	if !slices.Equal(stages[1].Lines, wantNotes) {
		t.Fatalf("notes lines = %q, want %q", stages[1].Lines, wantNotes)
	}
}

// The shipped template is all comments — nothing ports, nothing warns.
func TestCustomPortingAllCommentedTemplateYieldsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Dockerfile.custom")
	if err := os.WriteFile(path, []byte("# FROM opencode-sandbox-rust\n# RUN apt-get update\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if stages := customPorting(path); len(stages) != 0 {
		t.Fatalf("stages = %v, want none (the commented template stays inert)", stages)
	}
}

// A missing file (the common case — the file stops shipping) yields
// nothing: the warning row never renders for stage-less hosts.
func TestCustomPortingMissingFileYieldsNothing(t *testing.T) {
	if stages := customPorting(filepath.Join(t.TempDir(), "Dockerfile.custom")); len(stages) != 0 {
		t.Fatalf("stages = %v, want none for a missing file", stages)
	}
}

// Stages not named opencode-sandbox-<id> (helper stages, the FROM line
// of a plain recipe) are boundaries only — they port to nothing.
func TestCustomPortingIgnoresForeignStageNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Dockerfile.custom")
	if err := os.WriteFile(path, []byte(
		"FROM ubuntu:24.04 AS helper\nRUN helper-things\nFROM opencode-sandbox-base AS opencode-sandbox-notes\nRUN notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stages := customPorting(path)
	if len(stages) != 1 || stages[0].ID != "notes" || !slices.Equal(stages[0].Lines, []string{"RUN notes"}) {
		t.Fatalf("stages = %v, want only the opencode-sandbox-notes stage", stages)
	}
}
