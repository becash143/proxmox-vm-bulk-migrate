package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"vm-migrate/internal/model"
)

func TestSaveAndReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open on nonexistent file: %v", err)
	}

	st.UpsertWave(Wave{ID: "wave-1", Name: "frontend", CreatedAt: time.Now().UTC()})
	st.SetMapping(&Mapping{
		SourceMoRef: "vm-101",
		SourceName:  "web-01",
		WaveID:      "wave-1",
		Status:      StatusPlanned,
		Baseline:    &model.VSphereVM{Name: "web-01", NumCPU: 4, MemoryMB: 8192},
	})

	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	if len(reopened.Waves) != 1 || reopened.Waves[0].Name != "frontend" {
		t.Fatalf("wave not persisted correctly: %+v", reopened.Waves)
	}
	m, ok := reopened.Mappings["vm-101"]
	if !ok {
		t.Fatal("mapping vm-101 not found after reopen")
	}
	if m.Baseline == nil || m.Baseline.NumCPU != 4 {
		t.Fatalf("baseline not persisted correctly: %+v", m.Baseline)
	}

	mappings := reopened.MappingsInWave("wave-1")
	if len(mappings) != 1 {
		t.Fatalf("expected 1 mapping in wave-1, got %d", len(mappings))
	}
}

func TestOpenMissingFileReturnsEmptyStore(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if len(st.Waves) != 0 || len(st.Mappings) != 0 {
		t.Fatal("expected empty store")
	}
}

func TestAtomicSaveDoesNotLeaveTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	st, _ := Open(path)
	st.UpsertWave(Wave{ID: "w1", Name: "w1"})
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file should not remain after atomic save")
	}
}
