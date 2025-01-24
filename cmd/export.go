package cmd

import (
	"SimpleNosrtRelay/infra/config"
	"SimpleNosrtRelay/infra/log"
	"context"
	"fmt"
	"github.com/goccy/go-json"
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"time"

	"github.com/dgraph-io/badger/v4"
	eventstore "github.com/fiatjaf/eventstore/badger"
	"github.com/nbd-wtf/go-nostr"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export to json file",
	Long:  "Export all events from the database to a JSON file.",
	Run:   runExport,
}

func runExport(cmd *cobra.Command, args []string) {
	// Initialize configuration
	if err := config.InitConfig(); err != nil {
		log.Logger.Fatal("Failed to initialize configuration", zap.Error(err))
	}

	baseDir, err := filepath.Abs(config.Cfg.BasePath)
	if err != nil {
		log.Logger.Fatal("Failed to get absolute base path", zap.Error(err))
	}

	// Initialize logger
	log.Init()

	// Initialize Badger database
	store, err := initBadgerStore(baseDir)
	if err != nil {
		log.Logger.Fatal("Failed to initialize Badger store", zap.Error(err))
	}
	defer store.Close()
	store.Init()

	// Fetch all events
	events, err := fetchAllEvents(store)
	if err != nil {
		log.Logger.Fatal("Failed to fetch all events", zap.Error(err))
	}

	// Marshal events to JSON
	jsonData, err := json.Marshal(events)
	if err != nil {
		log.Logger.Fatal("Failed to marshal events to JSON", zap.Error(err))
	}
	exportPath := filepath.Join(baseDir, cmd.Flag("file").Value.String())

	// Write JSON data to file
	if err := writeJSONFile(exportPath, jsonData); err != nil {
		log.Logger.Fatal("Failed to write JSON data to file", zap.Error(err))
	}

	log.Logger.Info("Events exported to export.json")
}

// initBadgerStore initializes and returns a Badger event store.
func initBadgerStore(baseDir string) (*eventstore.BadgerBackend, error) {
	store := &eventstore.BadgerBackend{
		Path: filepath.Join(baseDir, "badger"),
		BadgerOptionsModifier: func(opts badger.Options) badger.Options {
			if config.Cfg.AppEnv == "production" {
				opts = opts.WithLoggingLevel(badger.WARNING)
			} else {
				opts = opts.WithLoggingLevel(badger.DEBUG)
			}

			opts.Logger = &log.DefaultLog{Logger: log.Logger}

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
	return store, nil
}

// fetchAllEvents retrieves all events from the event store.
func fetchAllEvents(store *eventstore.BadgerBackend) ([]*nostr.Event, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var events []*nostr.Event
	chanEvent, err := store.QueryEvents(ctx, nostr.Filter{LimitZero: true, Limit: 0})
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}

	for evt := range chanEvent {
		events = append(events, evt)
	}

	return events, nil
}

// writeJSONFile writes the given JSON data to the export file.
func writeJSONFile(exportPath string, data []byte) error {
	err := os.WriteFile(exportPath, data, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to write JSON file: %w", err)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(exportCmd)
	filename := fmt.Sprintf("export-%d.json", time.Now().Unix())
	exportCmd.Flags().StringP("file", "f", filename, "File to export events to")
}
