package manager

import (
	"SimpleNosrtRelay/infra/config"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dgraph-io/badger/v4"
	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip86"
	"github.com/nbd-wtf/go-nostr/sdk"
	"slices"
)

type ResourceType int8
type Manager struct {
	db *badger.DB
}

func NewManager(db *badger.DB) *Manager {
	return &Manager{db: db}
}

const (
	KindRelayAction              = 35000
	ResourceInvite  ResourceType = iota
	ResourceBlossom ResourceType = ResourceInvite + 1
	ResourceBan     ResourceType = ResourceBlossom + 1
)

var (
	ErrInvalidAction = errors.New("invalid action")
	ErrMissingTags   = errors.New("missing required tags")
	NoAccessError    = errors.New("no access to resource")
	NoInvited        = errors.New("not invited")
	NoResources      = errors.New("no resources")
)

type BanEvent struct {
	Reason string `json:"reason"`
}
type ResourceEvent struct {
	Access   bool         `json:"access"`
	Resource ResourceType `json:"resource"`
}

func (m *Manager) SaveEvent(ctx context.Context, event *nostr.Event) error {
	if event.Kind != KindRelayAction {
		return nil
	}

	action, target, relay, err := extractTags(event.Tags)
	if err != nil {
		return err
	}

	switch action {
	case "invite":
		return m.handleInvite(target, event)
	case "ban":
		return m.handleBan(target, event)
	case "authorize":
		return m.handleAuthorize(ctx, target, relay, event)
	default:
		return ErrInvalidAction
	}
}

// ValidateKind is a policy that validates the kind of an event
func ValidateKind(ctx context.Context, evt *nostr.Event) (bool, string) {
	if evt.Kind == KindRelayAction {
		if _, _, _, err := extractTags(evt.Tags); err != nil {
			return true, err.Error()
		}
	}
	return false, ""
}
func (m *Manager) handleInvite(target string, event *nostr.Event) error {
	var profile sdk.ProfileMetadata
	if err := json.Unmarshal([]byte(event.Content), &profile); err != nil {
		cont := event.Content
		if len(cont) > 100 {
			cont = cont[0:99]
		}
		return fmt.Errorf("failed to parse metadata (%s) from event %s: %w", cont, event.ID, err)
	}

	if err := m.ValidateResource(event.PubKey, ResourceInvite); err == nil {
		return m.saveInvited(target, profile)
	}
	return fmt.Errorf("failed to invite %s -> %s", event.PubKey, target)
}
func (m *Manager) handleBan(target string, event *nostr.Event) error {
	var banEvent BanEvent
	if err := json.Unmarshal([]byte(event.Content), &banEvent); err != nil {
		return err
	}
	if err := m.ValidateResource(event.PubKey, ResourceBan); err == nil {
		m.saveBan(target, banEvent)
		return m.deleteInvited(target)
	}
	return fmt.Errorf("failed to ban %s -> %s", event.PubKey, target)
}
func (m *Manager) handleAuthorize(ctx context.Context, target, relay string, event *nostr.Event) error {
	var resourceEvent ResourceEvent
	if err := json.Unmarshal([]byte(event.Content), &resourceEvent); err != nil {
		return err
	}
	currentResources, _ := m.queryResource(target)
	updatedResources := append(currentResources, resourceEvent)
	return m.saveResource(target, updatedResources)
}

func (m *Manager) CheckAccess(target string) error {
	if target == config.Cfg.Info.PubKey {
		return nil
	}
	_, err := m.queryInvited(target)
	if err != nil {
		return NoInvited
	}
	return nil
}
func (m *Manager) ValidateResource(target string, resource ResourceType) error {
	resources, err := m.queryResource(target)
	if err != nil {
		return NoResources
	}
	for _, res := range resources {
		if res.Resource == resource {
			return nil
		}
	}
	return NoAccessError
}

func extractTags(tags []nostr.Tag) (action, target, relay string, err error) {
	for _, tag := range tags {
		switch tag.Key() {
		case "action":
			if slices.Contains([]string{"invite", "ban", "authorize"}, tag.Value()) {
				action = tag.Value()
			}
		case "target":
			target = tag.Value()
		case "relay":
			relay = tag.Value()
		}
	}
	if action == "" || target == "" || relay == "" {
		return "", "", "", ErrMissingTags
	}
	return action, target, relay, nil
}
func (m *Manager) saveBan(target string, data BanEvent) error {
	key := []byte("ban:" + target)
	return m.db.Update(func(txn *badger.Txn) error {
		jdata, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return txn.Set(key, jdata)
	})
}
func (m *Manager) queryBan(target string) (*BanEvent, error) {
	key := []byte("ban:" + target)
	data := &BanEvent{}

	err := m.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, data)
		})
	})

	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, NoAccessError
	}

	return data, nil
}
func (m *Manager) saveResource(target string, data []ResourceEvent) error {
	key := []byte("resource:" + target)
	return m.db.Update(func(txn *badger.Txn) error {
		jdata, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return txn.Set(key, jdata)
	})
}
func (m *Manager) queryResource(target string) ([]ResourceEvent, error) {
	key := []byte("resource:" + target)
	var data []ResourceEvent
	err := m.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &data)
		})
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return []ResourceEvent{}, nil
	}
	return data, err
}
func (m *Manager) deleteResource(target string) error {
	key := []byte("resource:" + target)
	return m.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}
func (m *Manager) saveInvited(pubKey string, data sdk.ProfileMetadata) error {
	key := []byte("invited:" + pubKey)
	return m.db.Update(func(txn *badger.Txn) error {
		jdata, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return txn.Set(key, jdata)
	})
}
func (m *Manager) queryInvited(target string) (sdk.ProfileMetadata, error) {
	key := []byte("invited:" + target)
	var data sdk.ProfileMetadata
	err := m.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &data)
		})
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return sdk.ProfileMetadata{}, NoInvited
	}
	return data, err
}
func (m *Manager) deleteInvited(target string) error {
	key := []byte("invited:" + target)
	return m.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

func (m *Manager) RejectEvent() func(ctx context.Context, evt *nostr.Event) (bool, string) {
	return func(ctx context.Context, evt *nostr.Event) (bool, string) {

		if config.Cfg.AuthRequired {
			authenticatedUser := khatru.GetAuthed(ctx)
			if authenticatedUser == "" {
				return true, fmt.Sprintf("auth-required: %s", ErrMissingTags.Error())
			}
		}

		if evt.Kind == KindRelayAction {
			if _, _, _, err := extractTags(evt.Tags); err != nil {
				return true, err.Error()
			}
			authenticatedUser := khatru.GetAuthed(ctx)
			if authenticatedUser == "" {
				return true, fmt.Sprintf("auth-required: %s", ErrMissingTags.Error())
			}

			if err := m.CheckAccess(evt.PubKey); err != nil {
				return true, fmt.Sprintf("restricted: %s", err.Error())
			}
		}
		return false, ""
	}
}

func (m *Manager) ListBannedPubKeys() ([]nip86.PubKeyReason, error) {
	var banned []nip86.PubKeyReason
	err := m.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("ban:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := item.Key()
			var ban BanEvent
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &ban)
			}); err != nil {
				return err
			}
			banned = append(banned, nip86.PubKeyReason{PubKey: string(key[4:]), Reason: ban.Reason})
		}
		return nil
	})
	return banned, err
}
