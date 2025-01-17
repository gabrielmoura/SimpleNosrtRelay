package stream

import (
	"SimpleNosrtRelay/infra/log"
	"context"
	"github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"

	"sync"
	"time"
)

var relayInitOnce sync.Once

type RelaPool struct {
	StreamPoll []*nostr.Relay
	Relays     []string
	msg        chan nostr.Event
}

// ForwardEvent encaminha eventos para os relays e processa os eventos.
func (r *RelaPool) ForwardEvent() func(ctx context.Context, event *nostr.Event) error {
	return func(ctx context.Context, event *nostr.Event) error {
		if len(r.StreamPoll) > 0 {
			r.msg <- *event
		}
		return nil
	}
}

// PublishEvent encaminha eventos para os relays e processa os eventos.
func (r *RelaPool) PublishEvent() {
	for {
		select {
		case event := <-r.msg:
			for _, relay := range r.StreamPoll {
				if err := relay.Publish(relay.Context(), event); err != nil {
					log.Logger.Error(
						"failed to Forward event",
						zap.String("rs", relay.URL),
						zap.String("ID", event.ID),
						zap.Error(err),
					)
					continue
				}
				log.Logger.Debug(
					"Event forwarded",
					zap.String("ID", event.ID),
					zap.String("rs", relay.URL),
				)
			}
		default:
			time.Sleep(1 * time.Second)
		}

	}
}

func InitStream(rls *RelaPool) *RelaPool {
	initializeRelays(rls)
	return rls
}

// initializeRelays initializes relay connections if not already done.
func initializeRelays(rls *RelaPool) {
	relayInitOnce.Do(func() {
		for _, relayURL := range rls.Relays {
			connectRelay(rls, relayURL)
		}
	})
}

// connectRelay establishes a connection to the relay and adds it to the pool.
func connectRelay(rls *RelaPool, relayURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	relay, err := nostr.RelayConnect(ctx, relayURL)
	if err != nil {
		log.Logger.Error("Falha ao conectar no relay", zap.String("relay", relayURL), zap.Error(err))
		return
	}

	rls.StreamPoll = append(rls.StreamPoll, relay)
	log.Logger.Info("Conexão estabelecida com relay", zap.String("relay", relayURL))
}
