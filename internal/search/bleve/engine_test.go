package bleve

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/search"
)

func createTestConfig(t *testing.T) (*search.Config, string) {
	tmpDir, err := os.MkdirTemp("", "bleve-test-*")
	if err != nil {
		t.Fatal(err)
	}

	cfg := &search.Config{
		Enabled:          true,
		Engine:           search.EngineBleve,
		IndexPath:        tmpDir + "/test.bleve",
		Realtime:         true,
		BatchSize:        100,
		FlushInterval:    "100ms",
		Timeout:          "5s",
		FuzzyEnabled:     true,
		FuzzyDistance:    2,
		HighlightEnabled: true,
		MaxResults:       1000,
		Workers:          2,
	}

	return cfg, tmpDir
}

func TestBleveEngine_BasicOperations(t *testing.T) {
	cfg, tmpDir := createTestConfig(t)
	defer os.RemoveAll(tmpDir)

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close()

	ctx := context.Background()

	// Test indexing
	doc := &search.EmailDocument{
		ID:        "1:1",
		UserID:    1,
		MailboxID: 1,
		UID:       1,
		Subject:   "Test email about invoices",
		From:      "sender@example.com",
		To:        []string{"recipient@example.com"},
		BodyText:  "This is a test email body with important invoice information.",
		Date:      time.Now(),
	}

	err = engine.Index(ctx, doc)
	if err != nil {
		t.Fatalf("Failed to index document: %v", err)
	}

	// Test search
	query := &search.SearchQuery{
		Text:      "invoice",
		MailboxID: 1,
		UserID:    1,
	}

	result, err := engine.Search(ctx, query)
	if err != nil {
		t.Fatalf("Failed to search: %v", err)
	}

	if result.Total != 1 {
		t.Errorf("Expected 1 result, got %d", result.Total)
	}

	// Test delete
	err = engine.Delete(ctx, "1:1")
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	// Verify deletion
	result, err = engine.Search(ctx, query)
	if err != nil {
		t.Fatalf("Failed to search after delete: %v", err)
	}

	if result.Total != 0 {
		t.Errorf("Expected 0 results after delete, got %d", result.Total)
	}
}

func TestBleveEngine_BatchIndexing(t *testing.T) {
	cfg, tmpDir := createTestConfig(t)
	defer os.RemoveAll(tmpDir)

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close()

	ctx := context.Background()

	// Create batch of documents
	docs := make([]*search.EmailDocument, 100)
	for i := 0; i < 100; i++ {
		docs[i] = &search.EmailDocument{
			ID:        fmt.Sprintf("1:%d", i+1),
			UserID:    1,
			MailboxID: 1,
			UID:       uint32(i + 1),
			Subject:   fmt.Sprintf("Test email %d about topic%d", i, i%10),
			From:      "sender@example.com",
			To:        []string{"recipient@example.com"},
			BodyText:  fmt.Sprintf("Body content for email %d with keyword%d", i, i%5),
			Date:      time.Now().Add(-time.Duration(i) * time.Hour),
		}
	}

	// Batch index
	start := time.Now()
	err = engine.IndexBatch(ctx, docs)
	indexTime := time.Since(start)

	if err != nil {
		t.Fatalf("Failed to batch index: %v", err)
	}

	t.Logf("Batch indexed 100 documents in %v (%.2f docs/sec)", indexTime, 100.0/indexTime.Seconds())

	// Test search performance
	start = time.Now()
	result, err := engine.Search(ctx, &search.SearchQuery{
		Text:      "topic5",
		MailboxID: 1,
	})
	searchTime := time.Since(start)

	if err != nil {
		t.Fatalf("Failed to search: %v", err)
	}

	t.Logf("Search completed in %v, found %d results", searchTime, result.Total)
}

func BenchmarkBleveEngine_Index(b *testing.B) {
	cfg := &search.Config{
		Enabled:          true,
		Engine:           search.EngineBleve,
		IndexPath:        b.TempDir() + "/bench.bleve",
		FuzzyEnabled:     true,
		FuzzyDistance:    2,
		HighlightEnabled: false,
		MaxResults:       1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer engine.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc := &search.EmailDocument{
			ID:        fmt.Sprintf("1:%d", i),
			UserID:    1,
			MailboxID: 1,
			UID:       uint32(i),
			Subject:   "Test email about invoices and payments",
			From:      "sender@example.com",
			To:        []string{"recipient@example.com"},
			BodyText:  "This is a test email body with important invoice information for the quarterly report.",
			Date:      time.Now(),
		}
		engine.Index(ctx, doc)
	}
}

func BenchmarkBleveEngine_BatchIndex100(b *testing.B) {
	cfg := &search.Config{
		Enabled:          true,
		Engine:           search.EngineBleve,
		IndexPath:        b.TempDir() + "/bench.bleve",
		FuzzyEnabled:     true,
		FuzzyDistance:    2,
		HighlightEnabled: false,
		MaxResults:       1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer engine.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		docs := make([]*search.EmailDocument, 100)
		for j := 0; j < 100; j++ {
			docs[j] = &search.EmailDocument{
				ID:        fmt.Sprintf("1:%d", i*100+j),
				UserID:    1,
				MailboxID: 1,
				UID:       uint32(i*100 + j),
				Subject:   fmt.Sprintf("Test email %d about invoices", j),
				From:      "sender@example.com",
				To:        []string{"recipient@example.com"},
				BodyText:  "This is a test email body with important invoice information.",
				Date:      time.Now(),
			}
		}
		engine.IndexBatch(ctx, docs)
	}
}

func BenchmarkBleveEngine_Search(b *testing.B) {
	cfg := &search.Config{
		Enabled:          true,
		Engine:           search.EngineBleve,
		IndexPath:        b.TempDir() + "/bench.bleve",
		FuzzyEnabled:     true,
		FuzzyDistance:    2,
		HighlightEnabled: false,
		MaxResults:       1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer engine.Close()

	ctx := context.Background()

	// Pre-populate with 10,000 documents
	for batch := 0; batch < 100; batch++ {
		docs := make([]*search.EmailDocument, 100)
		for j := 0; j < 100; j++ {
			idx := batch*100 + j
			docs[j] = &search.EmailDocument{
				ID:        fmt.Sprintf("1:%d", idx),
				UserID:    1,
				MailboxID: 1,
				UID:       uint32(idx),
				Subject:   fmt.Sprintf("Email about topic%d category%d", idx%50, idx%10),
				From:      fmt.Sprintf("sender%d@example.com", idx%20),
				To:        []string{"recipient@example.com"},
				BodyText:  fmt.Sprintf("Content with keyword%d and phrase%d for searching", idx%100, idx%25),
				Date:      time.Now().Add(-time.Duration(idx) * time.Minute),
			}
		}
		engine.IndexBatch(ctx, docs)
	}

	queries := []string{"topic10", "keyword50", "phrase10", "category5", "invoice"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		query := &search.SearchQuery{
			Text:      queries[i%len(queries)],
			MailboxID: 1,
			Limit:     100,
		}
		engine.Search(ctx, query)
	}
}

func BenchmarkBleveEngine_SearchLargeIndex(b *testing.B) {
	cfg := &search.Config{
		Enabled:          true,
		Engine:           search.EngineBleve,
		IndexPath:        b.TempDir() + "/bench-large.bleve",
		FuzzyEnabled:     false, // Disable for speed
		HighlightEnabled: false,
		MaxResults:       100,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		b.Fatal(err)
	}
	defer engine.Close()

	ctx := context.Background()

	// Pre-populate with 100,000 documents (simulating 100k emails)
	b.Log("Indexing 100,000 documents...")
	indexStart := time.Now()
	for batch := 0; batch < 1000; batch++ {
		docs := make([]*search.EmailDocument, 100)
		for j := 0; j < 100; j++ {
			idx := batch*100 + j
			docs[j] = &search.EmailDocument{
				ID:        fmt.Sprintf("1:%d", idx),
				UserID:    1,
				MailboxID: 1,
				UID:       uint32(idx),
				Subject:   fmt.Sprintf("Email about topic%d category%d project%d", idx%50, idx%10, idx%100),
				From:      fmt.Sprintf("sender%d@example.com", idx%100),
				To:        []string{fmt.Sprintf("recipient%d@example.com", idx%50)},
				BodyText:  fmt.Sprintf("This is email content with keyword%d phrase%d and term%d for full text searching capabilities", idx%200, idx%50, idx%1000),
				Date:      time.Now().Add(-time.Duration(idx) * time.Minute),
			}
		}
		engine.IndexBatch(ctx, docs)
	}
	indexDuration := time.Since(indexStart)
	b.Logf("Indexed 100,000 documents in %v (%.0f docs/sec)", indexDuration, 100000.0/indexDuration.Seconds())

	queries := []string{"topic25", "keyword100", "phrase25", "project50", "email content"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		query := &search.SearchQuery{
			Text:      queries[i%len(queries)],
			MailboxID: 1,
			Limit:     100,
		}
		engine.Search(ctx, query)
	}
}
