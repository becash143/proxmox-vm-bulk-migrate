// Package state is the durable planning layer. Steampipe tables are
// read-only facts about what exists right now; none of that survives
// between runs and none of it can hold a decision ("this VM is in
// wave 2"). This package is deliberately simple -- a single JSON file
// with file locking -- because the state itself is small (hundreds to
// low thousands of VMs) and the priority is zero external
// dependencies over concurrent-write throughput. Swap this for a real
// Postgres-backed store if/when this becomes a multi-user hosted tool.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"vm-migrate/internal/model"
)

type MappingStatus string

const (
	StatusPlanned    MappingStatus = "planned"
	StatusInProgress MappingStatus = "in_progress"
	StatusMigrated   MappingStatus = "migrated"
	StatusValidated  MappingStatus = "validated"
	StatusFailed     MappingStatus = "failed"
	StatusRolledBack MappingStatus = "rolled_back"
)

// Wave groups VMs that migrate together (an app tier, a maintenance
// window, a risk tranche -- whatever the operator chooses).
type Wave struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	PlannedDate string    `json:"planned_date,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Mapping is the spine of the whole tool: one row per VM being
// migrated, tracking source identity, target identity, wave
// membership, and lifecycle status. Matching by MoRef (not name) is
// what survives VM renames during migration.
type Mapping struct {
	SourceMoRef string           `json:"source_moref"`
	SourceName  string           `json:"source_name"`
	WaveID      string           `json:"wave_id"`
	TargetNode  string           `json:"target_node,omitempty"`
	TargetVMID  int              `json:"target_vmid,omitempty"`
	Status      MappingStatus    `json:"status"`
	ESXiStorage string           `json:"esxi_storage,omitempty"` // storage ID registered in Proxmox pointing at the source ESXi host
	ESXiVMPath  string           `json:"esxi_vm_path,omitempty"` // path Proxmox reports for this VM under that storage
	UPID        string           `json:"upid,omitempty"`         // last Proxmox task ID for this migration
	Baseline    *model.VSphereVM `json:"baseline,omitempty"`     // snapshot taken at plan time, used for drift comparison
	LastError   string           `json:"last_error,omitempty"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// DriftFinding is one field-level mismatch discovered after import.
type DriftFinding struct {
	SourceMoRef string    `json:"source_moref"`
	Field       string    `json:"field"`
	Source      string    `json:"source_value"`
	Target      string    `json:"target_value"`
	CheckedAt   time.Time `json:"checked_at"`
}

// Store is the on-disk state document.
type Store struct {
	Waves     []Wave                   `json:"waves"`
	Mappings  map[string]*Mapping      `json:"mappings"` // keyed by source_moref
	Drift     []DriftFinding           `json:"drift"`
	Inventory *model.InventorySnapshot `json:"inventory,omitempty"`

	path string
	mu   sync.Mutex
}

// Open loads state from path, creating an empty store if the file
// doesn't exist yet.
func Open(path string) (*Store, error) {
	s := &Store{
		Mappings: map[string]*Mapping{},
		path:     path,
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w", err)
	}
	if len(b) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("parsing state file %s: %w", path, err)
	}
	if s.Mappings == nil {
		s.Mappings = map[string]*Mapping{}
	}
	s.path = path
	return s, nil
}

// Save writes the store back to disk atomically (write to temp file,
// rename over the original) so a crash mid-write can't corrupt state.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return fmt.Errorf("writing temp state file: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("committing state file: %w", err)
	}
	return nil
}

func (s *Store) UpsertWave(w Wave) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.Waves {
		if existing.ID == w.ID {
			s.Waves[i] = w
			return
		}
	}
	s.Waves = append(s.Waves, w)
}

func (s *Store) WaveByName(name string) (*Wave, bool) {
	for i := range s.Waves {
		if s.Waves[i].Name == name {
			return &s.Waves[i], true
		}
	}
	return nil, false
}

func (s *Store) SetMapping(m *Mapping) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m.UpdatedAt = time.Now().UTC()
	s.Mappings[m.SourceMoRef] = m
}

func (s *Store) MappingsInWave(waveID string) []*Mapping {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Mapping
	for _, m := range s.Mappings {
		if m.WaveID == waveID {
			out = append(out, m)
		}
	}
	return out
}

func (s *Store) AddDrift(f DriftFinding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Drift = append(s.Drift, f)
}
