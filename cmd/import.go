package cmd

import (
	"SimpleNosrtRelay/infra/config"
	"SimpleNosrtRelay/infra/log"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/eventstore/bluge"
	"github.com/fiatjaf/eventstore/mmm/betterbinary"
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
	importCmd.Flags().StringP("file", "f", "events.jsonl", "JSONL file to import")
}

func runImport(cmd *cobra.Command, _ []string) {
	// Initialize configuration and logger
	if err := config.InitConfig(); err != nil {
		log.Logger.Fatal("Failed to initialize configuration", zap.Error(err))
	}
	log.Init()

	// Get absolute base directory
	absBaseDir, err := getAbsBaseDir()
	if err != nil {
		log.Logger.Fatal("Failed to get absolute base path", zap.Error(err))
	}

	filename := cmd.Flag("file").Value.String()

	fileType, err := validateFileType(filename)
	if err != nil {
		log.Logger.Fatal("Invalid file type", zap.Error(err))
	}

	// Initialize event store and search index
	store, search, err := initDataStores(absBaseDir)
	if err != nil {
		log.Logger.Fatal("Failed to initialize data stores", zap.Error(err))
	}
	defer func() {
		store.Close()
		search.Close()
	}()

	if err := importEventsFromFile(filename, fileType, store, search); err != nil {
		log.Logger.Fatal("Failed to import events", zap.Error(err))
	}
}

func getAbsBaseDir() (string, error) {
	baseDir := config.Cfg.BasePath
	if baseDir == "" {
		baseDir = "."
	}
	return filepath.Abs(baseDir)
}

func initDataStores(baseDir string) (eventstore.Store, *bluge.BlugeBackend, error) {
	store, err := initBadgerStore(baseDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize Badger store: %w", err)
	}

	if err := store.Init(); err != nil {
		return nil, nil, fmt.Errorf("failed to initialize event store: %w", err)
	}

	search, err := initBlugeSearch(baseDir, store)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize search index: %w", err)
	}
	return store, search, nil
}

func validateFileType(filename string) (string, error) {
	if len(filename) < 5 {
		return "", fmt.Errorf("invalid file type: %s", filename)
	}

	lastIndex := len(filename)

	//Verifica se o arquivo termina com ".jsonl"
	if lastIndex >= 6 && filename[lastIndex-6:] == ".jsonl" {
		return "jsonl", nil
	}
	//Verifica se o arquivo termina com ".json"
	if lastIndex >= 5 && filename[lastIndex-5:] == ".json" {
		return "json", nil
	}

	return "", fmt.Errorf("invalid file type: %s", filename)
}

func initBlugeSearch(baseDir string, store eventstore.Store) (*bluge.BlugeBackend, error) {
	search := bluge.BlugeBackend{Path: filepath.Join(baseDir, "search"), RawEventStore: store}
	if err := search.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize bluge search index: %w", err)
	}
	return &search, nil
}

func importEventsFromFile(filename, fileType string, store eventstore.Store, search *bluge.BlugeBackend) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filename, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Logger.Error("Failed to close file", zap.String("filename", filename), zap.Error(err))
		}
	}()

	reader := bufio.NewReaderSize(file, 1024*1024)
	counter := 0

	if fileType == "jsonl" {
		counter, err = importFromJSONL(reader, store, search)
		if err != nil {
			return err
		}
	} else if fileType == "json" {
		counter, err = importFromJSON(reader, store, search)
		if err != nil {
			return err
		}
	}
	log.Logger.Info("Imported events", zap.Int("count", counter))
	fmt.Printf("Successfully imported %d events from %s\n", counter, filename)
	return nil
}

func importFromJSONL(reader *bufio.Reader, store eventstore.Store, search *bluge.BlugeBackend) (int, error) {
	counter := 0
	for {
		line, err := readLine(reader)
		if err != nil {
			if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) {
				break // Exit loop if file reading ended
			}
			log.Logger.Error("Failed to read line", zap.Error(err))
			continue // Skip invalid lines
		}

		if strings.TrimSpace(string(line)) == "" {
			log.Logger.Debug("Skipping empty line")
			continue // Skip empty lines
		}

		event, err := unmarshalEvent(line)
		if err != nil {
			log.Logger.Error("Failed to unmarshal event", zap.Error(err), zap.String("line", string(line)))
			continue
		}

		if !isValidEvent(event) {
			continue
		}

		if err := saveEvent(context.Background(), store, search, event); err != nil {
			if errors.Is(err, eventstore.ErrDupEvent) {
				continue
			}
			log.Logger.Error("Failed to save event", zap.Error(err), zap.String("event_id", event.ID))
			return counter, err
		}
		counter++
	}
	return counter, nil
}

func importFromJSON(reader *bufio.Reader, store eventstore.Store, search *bluge.BlugeBackend) (int, error) {
	data, _ := reader.ReadBytes('\n')
	var events []nostr.Event
	if err := json.Unmarshal(data, &events); err != nil {
		return 0, fmt.Errorf("failed to unmarshal json array: %w", err)
	}
	counter := 0
	for _, event := range events {
		if !isValidEvent(&event) {
			continue
		}

		if err := saveEvent(context.Background(), store, search, &event); err != nil {
			if errors.Is(err, eventstore.ErrDupEvent) {
				continue
			}
			log.Logger.Error("Failed to save event", zap.Error(err), zap.String("event_id", event.ID))
			return counter, err
		}
		counter++
	}
	return counter, nil
}

func isValidEvent(event *nostr.Event) bool {
	if len(event.Content) > betterbinary.MaxContentSize {
		log.Logger.Error("content is too large", zap.Int("size", len(event.Content)), zap.String("ID", event.ID))
		return false
	}

	if _, err := event.CheckSignature(); err != nil {
		log.Logger.Debug("Invalid signature", zap.Error(err))
		return false
	}
	return true
}

func readLine(reader *bufio.Reader) ([]byte, error) {
	line, _, err := reader.ReadLine()
	return line, err
}

func unmarshalEvent(line []byte) (*nostr.Event, error) {
	var event nostr.Event
	if err := json.Unmarshal(line, &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event: %w", err)
	}
	return &event, nil
}

func saveEvent(ctx context.Context, store eventstore.Store, search *bluge.BlugeBackend, event *nostr.Event) error {
	if err := store.SaveEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to save event to event store: %w", err)
	}
	if err := search.SaveEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to save event to search index: %w", err)
	}
	return nil
}
