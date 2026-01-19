package bleve

import (
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/keyword"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/simple"
	"github.com/blevesearch/bleve/v2/analysis/lang/en"
	"github.com/blevesearch/bleve/v2/mapping"
)

// CreateIndexMapping creates the Bleve index mapping for email documents.
func CreateIndexMapping() mapping.IndexMapping {
	// Create a new index mapping
	indexMapping := bleve.NewIndexMapping()

	// Create document mapping for email
	emailMapping := bleve.NewDocumentMapping()

	// ID field - stored but not indexed for search
	idField := bleve.NewTextFieldMapping()
	idField.Store = true
	idField.Index = false
	emailMapping.AddFieldMappingsAt("id", idField)

	// UserID field - numeric, used for filtering
	userIDField := bleve.NewNumericFieldMapping()
	userIDField.Store = true
	userIDField.Index = true
	emailMapping.AddFieldMappingsAt("user_id", userIDField)

	// MailboxID field - numeric, used for filtering
	mailboxIDField := bleve.NewNumericFieldMapping()
	mailboxIDField.Store = true
	mailboxIDField.Index = true
	emailMapping.AddFieldMappingsAt("mailbox_id", mailboxIDField)

	// UID field - numeric, stored
	uidField := bleve.NewNumericFieldMapping()
	uidField.Store = true
	uidField.Index = true
	emailMapping.AddFieldMappingsAt("uid", uidField)

	// Subject field - full text search with English analyzer
	subjectField := bleve.NewTextFieldMapping()
	subjectField.Analyzer = en.AnalyzerName
	subjectField.Store = true
	subjectField.Index = true
	subjectField.IncludeTermVectors = true
	emailMapping.AddFieldMappingsAt("subject", subjectField)

	// From field - keyword analyzer for exact email matching
	fromField := bleve.NewTextFieldMapping()
	fromField.Analyzer = keyword.Name
	fromField.Store = true
	fromField.Index = true
	emailMapping.AddFieldMappingsAt("from", fromField)

	// To field - keyword analyzer for exact email matching
	toField := bleve.NewTextFieldMapping()
	toField.Analyzer = keyword.Name
	toField.Store = true
	toField.Index = true
	emailMapping.AddFieldMappingsAt("to", toField)

	// Cc field - keyword analyzer for exact email matching
	ccField := bleve.NewTextFieldMapping()
	ccField.Analyzer = keyword.Name
	ccField.Store = true
	ccField.Index = true
	emailMapping.AddFieldMappingsAt("cc", ccField)

	// BodyText field - main content, English analyzer with term vectors
	bodyTextField := bleve.NewTextFieldMapping()
	bodyTextField.Analyzer = en.AnalyzerName
	bodyTextField.Store = false // Don't store body to save space
	bodyTextField.Index = true
	bodyTextField.IncludeTermVectors = true
	emailMapping.AddFieldMappingsAt("body_text", bodyTextField)

	// BodyHTML field - stripped HTML content
	bodyHTMLField := bleve.NewTextFieldMapping()
	bodyHTMLField.Analyzer = en.AnalyzerName
	bodyHTMLField.Store = false
	bodyHTMLField.Index = true
	bodyHTMLField.IncludeTermVectors = true
	emailMapping.AddFieldMappingsAt("body_html", bodyHTMLField)

	// Date field - datetime for sorting and filtering
	dateField := bleve.NewDateTimeFieldMapping()
	dateField.Store = true
	dateField.Index = true
	emailMapping.AddFieldMappingsAt("date", dateField)

	// InternalDate field - IMAP internal date
	internalDateField := bleve.NewDateTimeFieldMapping()
	internalDateField.Store = true
	internalDateField.Index = true
	emailMapping.AddFieldMappingsAt("internal_date", internalDateField)

	// Flags field - keyword analyzer for exact matching
	flagsField := bleve.NewTextFieldMapping()
	flagsField.Analyzer = keyword.Name
	flagsField.Store = true
	flagsField.Index = true
	emailMapping.AddFieldMappingsAt("flags", flagsField)

	// MessageID field - keyword for exact matching
	messageIDField := bleve.NewTextFieldMapping()
	messageIDField.Analyzer = simple.Name
	messageIDField.Store = true
	messageIDField.Index = true
	emailMapping.AddFieldMappingsAt("message_id", messageIDField)

	// Size field - numeric
	sizeField := bleve.NewNumericFieldMapping()
	sizeField.Store = true
	sizeField.Index = true
	emailMapping.AddFieldMappingsAt("size", sizeField)

	// Register the email document mapping
	indexMapping.AddDocumentMapping("email", emailMapping)
	indexMapping.DefaultMapping = emailMapping

	// Set default analyzer
	indexMapping.DefaultAnalyzer = en.AnalyzerName

	return indexMapping
}
