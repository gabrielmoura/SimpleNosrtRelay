package main

import (
	"SimpleNosrtRelay/infra/blob"
	"SimpleNosrtRelay/infra/config"
	"SimpleNosrtRelay/infra/log"
	"SimpleNosrtRelay/infra/metrics"
	"SimpleNosrtRelay/infra/stream"
	"context"
	"fmt"
	"github.com/dgraph-io/badger/v4"
	eventstore "github.com/fiatjaf/eventstore/badger"
	"github.com/fiatjaf/eventstore/bluge"
	"go.uber.org/zap"
	"time"

	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/khatru/blossom"
	"github.com/fiatjaf/khatru/policies"
	"github.com/nbd-wtf/go-nostr"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"net/http"
	"path/filepath"
	"strconv"
)

func main() {
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
	relay.Info.Software = "nostr-relay-server"
	relay.Info.Version = "0.1.0"
	relay.Negentropy = config.Cfg.Negentropy

	relay.OnConnect = append(relay.OnConnect, khatru.RequestAuth)

	store := &eventstore.BadgerBackend{
		Path: filepath.Join(baseDir, "badger"),
		BadgerOptionsModifier: func(opts badger.Options) badger.Options {
			// Optimizations for pen-drivers
			opts.ValueLogFileSize = 5 << 20 // 5MB
			opts.NumVersionsToKeep = 3      // Keep only 3 versions of each key
			opts.NumGoroutines = 4          // Reduced to 4 goroutines to minimize context switching overhead

			opts.NumCompactors = 2            // Reduced to 2 compactors to minimize CPU usage
			opts.NumLevelZeroTables = 3       // Reduced to 3 to limit L0 table count
			opts.NumLevelZeroTablesStall = 10 // Reduced to 10 to trigger compactions sooner
			opts.NumMemtables = 3             // Reduced to 3 to limit memory usage
			opts.BlockCacheSize = 128 << 20   // Reduced to 128MB to fit within RAM

			return opts
		},
	}

	rls := stream.InitStream(&stream.RelaPool{
		Relays:     []string{},
		StreamPoll: make([]*nostr.Relay, 0),
	})

	go rls.PublishEvent()

	err := store.Init()
	if err != nil {
		//log.Println(err)
		log.Logger.Fatal("Erro ao iniciar conexão com o banco de dados", zap.Error(err))
		return
	}

	search := bluge.BlugeBackend{Path: filepath.Join(baseDir, "search"), RawEventStore: store}
	if err := search.Init(); err != nil {
		panic(err)
	}

	// set up the basic relay functions
	relay.StoreEvent = append(relay.StoreEvent, store.SaveEvent, func(ctx context.Context, event *nostr.Event) error {
		metrics.NostrKindEventCounter.WithLabelValues(strconv.Itoa(event.Kind)).Inc()

		for _, tag := range event.Tags {
			if tag.Key() == "t" {
				metrics.NostrTagEventCounter.WithLabelValues(tag.Value()).Inc()
			}
		}
		return nil
	}, search.SaveEvent, rls.ForwardEvent())
	relay.QueryEvents = append(relay.QueryEvents, func(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
		for _, kind := range filter.Kinds {
			metrics.NostrKindReqCounter.WithLabelValues(strconv.Itoa(kind)).Inc()
		}
		return store.QueryEvents(ctx, filter)
	}, search.QueryEvents)
	relay.DeleteEvent = append(relay.DeleteEvent, store.DeleteEvent, search.DeleteEvent)
	relay.ReplaceEvent = append(relay.ReplaceEvent, store.ReplaceEvent, search.ReplaceEvent)
	relay.CountEvents = append(relay.CountEvents, store.CountEvents)

	// there are many other configurable things you can set
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

				log.Logger.Info("Request from", zap.String("pubkey", pubkey))
				return false, ""
			}
			return true, "auth-required: only authenticated users can read from this relay"
			// (this will cause an AUTH message to be sent and then a CLOSED message such that clients can
			//  authenticate and then request again)
		},
	)
	// check the docs for more goodies!

	mux := relay.Router()
	// set up other http handlers
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/html")
		fmt.Fprintf(w, `<b>welcome</b> to my relay!`)
	})
	mux.Handle("/metrics", promhttp.Handler())

	bl := blossom.New(relay, relay.Info.URL)

	// create a database for keeping track of blob metadata
	bl.Store = blossom.EventStoreBlobIndexWrapper{
		Store:      store,
		ServiceURL: bl.ServiceURL,
	}

	bs := blob.NewBlobStore(store.DB, &blob.BlobConfig{
		BasePath:       filepath.Join(baseDir, "blobs"),
		ExtAcceptable:  []string{".jpg", ".gif", ".png", ".webp", ".mp4"},
		MaxFileSize:    10 * 1024 * 1024, // 10MB
		MimeAcceptable: []string{"image/png", "image/jpeg", "image/gif", "image/webp", "video/mp4", "video/webm", "video/ogg"},
		AuthRequired:   false,
	})

	bs.Init()

	// implement the required storage functions
	bl.StoreBlob = append(bl.StoreBlob, bs.StoreBlob)
	bl.LoadBlob = append(bl.LoadBlob, bs.LoadBlob)
	bl.DeleteBlob = append(bl.DeleteBlob, bs.DeleteBlob)
	bl.RejectUpload = append(bl.RejectUpload, bs.RejectUpload(getAuthed(store.DB)))

	// start the server
	log.Logger.Info("running on :3334")
	http.ListenAndServe(":3334", relay)
}
func getAuthed(db *badger.DB) func(auth *nostr.Event) bool {
	return func(auth *nostr.Event) bool {
		if auth.PubKey == config.Cfg.Info.PubKey {
			return true
		}
		txn := db.NewTransaction(false)
		defer txn.Discard()
		_, err := txn.Get([]byte("authorized:" + auth.PubKey))
		if err != nil {
			return false
		}
		return true
	}
}
func setAuthed(db *badger.DB, pubKey string) error {
	txn := db.NewTransaction(true)
	defer txn.Discard()
	txn.Set([]byte("authorized:"+pubKey), []byte{})
	return txn.Commit()
}
