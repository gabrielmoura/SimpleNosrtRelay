package cmd

import (
	"SimpleNosrtRelay/infra/blob"
	"SimpleNosrtRelay/infra/config"
	"SimpleNosrtRelay/infra/log"
	"SimpleNosrtRelay/infra/manager"
	"SimpleNosrtRelay/infra/metrics"
	"SimpleNosrtRelay/infra/stream"
	"context"
	"fmt"
	"github.com/fiatjaf/eventstore/bluge"
	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/khatru/blossom"
	"github.com/fiatjaf/khatru/policies"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip86"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"net/http"
	"path/filepath"
	"strconv"
	"time"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Starts a Nostr Relay Server",
	Long:  `Starts a Nostr Relay Server that receives messages from a Nostr Client and forwards them to a Nostr Server.`,
	Run:   runServer,
}

func runServer(cmd *cobra.Command, args []string) {
	if err := config.InitConfig(); err != nil {
		panic(err)
		return
	}

	baseDir, _ := filepath.Abs(config.Cfg.BasePath)

	log.Init()

	metrics.RegisterMetrics()

	// create the relay instance
	relay := khatru.NewRelay()

	// set up some basic properties (will be returned on the NIP-11 endpoint)
	relay.Info.Name = config.Cfg.Info.Name
	relay.Info.PubKey = config.Cfg.Info.PubKey
	relay.Info.Description = config.Cfg.Info.Description
	relay.Info.Icon = config.Cfg.Info.Icon
	relay.Info.URL = config.Cfg.Info.Url
	relay.Info.Software = "https://github.com/gabrielmoura/SimpleNosrtRelay"
	relay.Info.Version = "1.0.0"
	relay.Negentropy = config.Cfg.Negentropy

	relay.OnConnect = append(relay.OnConnect, khatru.RequestAuth)

	store, err := initBadgerStore(baseDir)
	if err != nil {
		log.Logger.Fatal("Failed to initialize Badger store", zap.Error(err))
	}

	m := manager.NewManager(store.DB)
	relay.ManagementAPI.ListBannedPubKeys = func(ctx context.Context) ([]nip86.PubKeyReason, error) {
		return m.ListBannedPubKeys()
	}

	rls := stream.InitStream(&stream.RelaPool{
		Relays:     config.Cfg.Stream.Relays,
		StreamPoll: make([]*nostr.Relay, 0),
	})

	go rls.PublishEvent()

	err = store.Init()
	if err != nil {
		//log.Println(err)
		log.Logger.Fatal("Erro ao iniciar conexão com o banco de dados", zap.Error(err))
		return
	}

	search := bluge.BlugeBackend{Path: filepath.Join(baseDir, "search"), RawEventStore: store}
	if err := search.Init(); err != nil {
		panic(err)
	}

	// StoreEvent is a list of functions that will be called in order to store an event
	relay.StoreEvent = append(relay.StoreEvent, store.SaveEvent, func(ctx context.Context, event *nostr.Event) error {
		metrics.NostrKindEventCounter.WithLabelValues(strconv.Itoa(event.Kind)).Inc()

		for _, tag := range event.Tags {
			if tag.Key() == "t" {
				metrics.NostrTagEventCounter.WithLabelValues(tag.Value()).Inc()
			}
		}
		return nil
	}, search.SaveEvent, rls.ForwardEvent(), m.SaveEvent)

	// QueryEvents is a list of functions that will be called in order to query events
	relay.QueryEvents = append(relay.QueryEvents, func(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
		for _, kind := range filter.Kinds {
			metrics.NostrKindReqCounter.WithLabelValues(strconv.Itoa(kind)).Inc()
		}
		return store.QueryEvents(ctx, filter)
	}, search.QueryEvents)

	// DeleteEvent is a list of functions that will be called in order to delete an event
	relay.DeleteEvent = append(relay.DeleteEvent, store.DeleteEvent, search.DeleteEvent)

	// ReplaceEvent is a list of functions that will be called in order to replace an event
	relay.ReplaceEvent = append(relay.ReplaceEvent, store.ReplaceEvent, search.ReplaceEvent)

	// CountEvents is a list of functions that will be called in order to count events
	relay.CountEvents = append(relay.CountEvents, store.CountEvents)

	// RejectEvent is a list of functions that will be called in order to reject an event
	relay.RejectEvent = append(relay.RejectEvent,
		// built-in policies
		policies.ValidateKind,

		// Rejeita eventos com timestamps no futuro além de 5 minutos
		policies.PreventTimestampsInTheFuture(5*time.Minute),

		// 10 eventos por segundo
		// Cria um limitador de taxa que permite até 10 eventos por segundo por chave pública,
		// com um máximo de 10 tokens no balde.
		policies.EventPubKeyRateLimiter(10, time.Second, 10),

		// define your own policies
		policies.PreventLargeTags(120),
		func(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
			if event.PubKey == "fa984bd7dbb282f07e16e7ae87b26a2a7b9b90b7246a44771f0cf5ae58018f52" {
				return true, "we don't allow this person to write here"
			}
			return false, "" // anyone else can
		},
		policies.RejectEventsWithBase64Media,
		m.RejectEvent(),
	)

	// you can request auth by rejecting an event or a request with the prefix "auth-required: "
	relay.RejectFilter = append(relay.RejectFilter,
		// built-in policies
		policies.NoComplexFilters,
		policies.NoEmptyFilters,
		policies.AntiSyncBots,

		// define your own policies
		func(ctx context.Context, filter nostr.Filter) (reject bool, msg string) {
			if pubkey := khatru.GetAuthed(ctx); pubkey != "" {
				log.Logger.Debug("Request from", zap.String("pubkey", pubkey), zap.String("filter", filter.String()))
			}
			return false, ""
		},
	)
	// check the docs for more goodies!

	mux := relay.Router()
	// set up other http handlers
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/html")
		fmt.Fprintf(w, `<b>welcome</b> to my relay! `+config.Cfg.Info.Url)
	})
	mux.Handle("/metrics", promhttp.Handler())

	bl := blossom.New(relay, relay.Info.URL)

	// create a database for keeping track of blob metadata
	bl.Store = blossom.EventStoreBlobIndexWrapper{
		Store:      store,
		ServiceURL: bl.ServiceURL,
	}

	bs := blob.NewBlobStore(store.DB, &blob.Config{
		BasePath:       filepath.Join(baseDir, "blobs"),
		ExtAcceptable:  []string{".jpg", ".gif", ".png", ".webp", ".mp4"},
		MaxFileSize:    10 * 1024 * 1024, // 10MB
		MimeAcceptable: []string{"image/png", "image/jpeg", "image/gif", "image/webp", "video/mp4", "video/webm", "video/ogg"},
		AuthRequired:   config.Cfg.Blossom.AuthRequired,
	})

	bs.Init()

	// implement the required storage functions
	bl.StoreBlob = append(bl.StoreBlob, bs.StoreBlob)
	bl.LoadBlob = append(bl.LoadBlob, bs.LoadBlob)
	bl.DeleteBlob = append(bl.DeleteBlob, bs.DeleteBlob)
	bl.RejectUpload = append(bl.RejectUpload, bs.RejectUpload(authorizeBlossom(m)))

	// start the server
	log.Logger.Info("running on :3334")
	http.ListenAndServe(":3334", relay)
}
func init() {
	rootCmd.AddCommand(serverCmd)
}
func authorizeBlossom(m *manager.Manager) func(auth *nostr.Event) bool {
	return func(auth *nostr.Event) bool {
		if auth.PubKey == config.Cfg.Info.PubKey {
			return true
		}
		err := m.ValidateResource(auth.PubKey, manager.ResourceBlossom)
		if err == nil {
			return true
		}
		return false
	}
}
