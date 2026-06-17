package exchange

import (
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/itaywol/adeptability/pkg/adept"
)

// snapshot is the serializable state of a board. The fs driver marshals it
// to JSON; the memory driver keeps it only in RAM.
type snapshot struct {
	BootstrapHash string               `json:"bootstrapHash"`
	NextID        int                  `json:"nextId"`
	Users         []adept.ExchangeUser `json:"users"`
	Items         []adept.ExchangeItem `json:"items"`
}

// memStore is the shared in-memory implementation. The fs driver embeds the
// same maps and supplies a persist hook for write-through durability.
//
// ponytail: global lock, fine for a team-sized board; shard per-item if a
// large org ever contends on it.
type memStore struct {
	mu       sync.Mutex
	now      func() time.Time
	persist  func(snapshot) error // nil for the memory driver
	bootHash string
	nextID   int
	users    []adept.ExchangeUser
	items    map[int]adept.ExchangeItem
}

// newMemStore builds a memStore, optionally seeding from a snapshot (used by
// the fs driver when loading an existing board file).
func newMemStore(s *snapshot) *memStore {
	m := &memStore{
		now:    func() time.Time { return time.Now().UTC() },
		nextID: 1,
		items:  map[int]adept.ExchangeItem{},
	}
	if s != nil {
		m.bootHash = s.BootstrapHash
		m.users = append(m.users, s.Users...)
		for _, it := range s.Items {
			m.items[it.ID] = it
		}
		m.nextID = max(s.NextID, 1)
	}
	return m
}

// snapshotLocked captures current state. Caller must hold mu.
func (m *memStore) snapshotLocked() snapshot {
	items := make([]adept.ExchangeItem, 0, len(m.items))
	for _, it := range m.items {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return snapshot{
		BootstrapHash: m.bootHash,
		NextID:        m.nextID,
		Users:         append([]adept.ExchangeUser{}, m.users...),
		Items:         items,
	}
}

// flushLocked persists current state if a hook is configured. Caller holds mu.
func (m *memStore) flushLocked() error {
	if m.persist == nil {
		return nil
	}
	return m.persist(m.snapshotLocked())
}

func (m *memStore) ts() string { return m.now().Format(time.RFC3339) }

func (m *memStore) BootstrapHash() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.bootHash, nil
}

func (m *memStore) SetBootstrapHash(hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bootHash = hash
	return m.flushLocked()
}

func (m *memStore) CreateUser(u adept.ExchangeUser) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ex := range m.users {
		if ex.Handle == u.Handle {
			return adept.ErrExchangeHandleTaken
		}
	}
	m.users = append(m.users, u)
	return m.flushLocked()
}

func (m *memStore) UserByTokenHash(hash string) (adept.ExchangeUser, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.TokenHash == hash {
			return u, true, nil
		}
	}
	return adept.ExchangeUser{}, false, nil
}

func (m *memStore) RotateUserToken(oldHash, newHash string) (adept.ExchangeUser, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, u := range m.users {
		if u.TokenHash == oldHash {
			m.users[i].TokenHash = newHash
			return m.users[i], true, m.flushLocked()
		}
	}
	return adept.ExchangeUser{}, false, nil
}

func (m *memStore) CreateItem(item adept.ExchangeItem) (adept.ExchangeItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item.ID = m.nextID
	m.nextID++
	item.Status = adept.ExchangeDefaultStatus
	now := m.ts()
	item.CreatedAt = now
	item.UpdatedAt = now
	item.Comments = nil
	m.items[item.ID] = item
	if err := m.flushLocked(); err != nil {
		return adept.ExchangeItem{}, err
	}
	return item, nil
}

func (m *memStore) GetItem(id int) (adept.ExchangeItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return adept.ExchangeItem{}, adept.ErrExchangeItemNotFound
	}
	return it, nil
}

func (m *memStore) ListItems(f ListFilter) ([]adept.ExchangeItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]adept.ExchangeItem, 0, len(m.items))
	for _, it := range m.items {
		if f.Status != "" && it.Status != f.Status {
			continue
		}
		if f.Mine && !isMine(it, f.Handle) {
			continue
		}
		out = append(out, it)
	}
	// Newest first by ID (IDs are monotonic).
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (m *memStore) AddComment(id int, c adept.ExchangeComment) (adept.ExchangeItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return adept.ExchangeItem{}, adept.ErrExchangeItemNotFound
	}
	c.CreatedAt = m.ts()
	it.Comments = append(it.Comments, c)
	// First response auto-flips an open request to in-progress.
	if it.Status == adept.ExchangeStatusAttention {
		it.Status = adept.ExchangeStatusInProgress
	}
	it.UpdatedAt = m.ts()
	m.items[id] = it
	if err := m.flushLocked(); err != nil {
		return adept.ExchangeItem{}, err
	}
	return it, nil
}

func (m *memStore) SetStatus(id int, status string) (adept.ExchangeItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return adept.ExchangeItem{}, adept.ErrExchangeItemNotFound
	}
	it.Status = status
	it.UpdatedAt = m.ts()
	m.items[id] = it
	if err := m.flushLocked(); err != nil {
		return adept.ExchangeItem{}, err
	}
	return it, nil
}

// isMine reports whether handle authored or is assigned to the item.
func isMine(it adept.ExchangeItem, handle string) bool {
	return it.Author == handle || slices.Contains(it.Assignees, handle)
}
