package cmd

import (
	"SimpleNosrtRelay/infra/config"
	"SimpleNosrtRelay/infra/log"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/fiatjaf/eventstore/bluge"
	"os"
	"path/filepath"

	"github.com/nbd-wtf/go-nostr"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import from a JSON file",
	Long:  "Imports Nostr events from a JSON file, one event per line.",
	Run:   runImport,
}

func init() {
	rootCmd.AddCommand(importCmd)
	importCmd.Flags().StringP("file", "f", "events.json", "JSON file to import")
}

func runImport(cmd *cobra.Command, args []string) {
	// Initialize configuration
	if err := config.InitConfig(); err != nil {
		log.Logger.Fatal("Failed to initialize configuration", zap.Error(err))
	}

	// Initialize logger
	log.Init()

	baseDir := config.Cfg.BasePath
	if baseDir == "" {
		baseDir = "." // Use current directory as default
	}
	absBaseDir, err := filepath.Abs(baseDir)
	if err != nil {
		log.Logger.Fatal("Failed to get absolute base path", zap.Error(err))
	}

	// Initialize Badger database
	store, err := initBadgerStore(absBaseDir)
	if err != nil {
		log.Logger.Fatal("Failed to initialize Badger store", zap.Error(err))
	}

	store.Init()

	search := bluge.BlugeBackend{Path: filepath.Join(baseDir, "search"), RawEventStore: store}
	if err := search.Init(); err != nil {
		panic(err)
	}
	defer func() {
		search.Close()
		store.Close()
	}()

	filename := cmd.Flag("file").Value.String()

	file, err := os.Open(filename)
	if err != nil {
		log.Logger.Fatal("Failed to open file", zap.String("filename", filename), zap.Error(err))
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Logger.Error("Failed to close file", zap.String("filename", filename), zap.Error(err))
		}
	}()

	reader := bufio.NewReader(file)
	var counter int
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			// Remove trailing newline character
			line = line[:len(line)-1]
		}

		if err != nil {
			if err.Error() == "EOF" {
				break // End of file
			}
			log.Logger.Error("Failed to read line", zap.Error(err))
			continue // Skip invalid lines
		}

		var event nostr.Event
		if err := json.Unmarshal(line, &event); err != nil {
			log.Logger.Error("Failed to unmarshal event", zap.Error(err), zap.String("line", string(line)))
			continue // Skip invalid json lines
		}

		if err := store.SaveEvent(context.Background(), &event); err != nil {
			log.Logger.Error("Failed to save event", zap.Error(err), zap.String("event_id", event.ID))
			continue // Skip saving events with erros
		}
		if err = search.SaveEvent(context.Background(), &event); err != nil {
			return
		}

		counter++
	}

	log.Logger.Info("Imported events", zap.Int("count", counter))
	fmt.Printf("Successfully imported %d events from %s\n", counter, filename)

}
