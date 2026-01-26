package helpers

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/fenilsonani/email-server/internal/auth"
)

// MockAuthenticator provides a mock implementation of the Authenticator for testing.
type MockAuthenticator struct {
	mu              sync.RWMutex
	users           map[string]*auth.User
	authenticateErr error
	lookupUserErr   error
}

// NewMockAuthenticator creates a new mock authenticator.
func NewMockAuthenticator() *MockAuthenticator {
	return &MockAuthenticator{
		users: make(map[string]*auth.User),
	}
}

// AddUser adds a user to the mock authenticator.
func (m *MockAuthenticator) AddUser(user *auth.User) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[user.Email] = user
}

// SetAuthenticateError sets an error to be returned on Authenticate calls.
func (m *MockAuthenticator) SetAuthenticateError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authenticateErr = err
}

// SetLookupUserError sets an error to be returned on LookupUser calls.
func (m *MockAuthenticator) SetLookupUserError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lookupUserErr = err
}

// Authenticate returns a user if found and no error is set.
func (m *MockAuthenticator) Authenticate(ctx context.Context, email, password string) (*auth.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.authenticateErr != nil {
		return nil, m.authenticateErr
	}

	user, exists := m.users[email]
	if !exists {
		return nil, errors.New("user not found")
	}

	return user, nil
}

// LookupUser returns a user if found and no error is set.
func (m *MockAuthenticator) LookupUser(ctx context.Context, email string) (*auth.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.lookupUserErr != nil {
		return nil, m.lookupUserErr
	}

	user, exists := m.users[email]
	if !exists {
		return nil, errors.New("user not found")
	}

	return user, nil
}

// ValidateAddress validates an email address (mock implementation).
func (m *MockAuthenticator) ValidateAddress(ctx context.Context, email string) error {
	if email == "" {
		return errors.New("email cannot be empty")
	}
	return nil
}

// UpdatePassword updates a user's password (mock implementation).
func (m *MockAuthenticator) UpdatePassword(ctx context.Context, userID int64, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, user := range m.users {
		if user.ID == userID {
			// In real implementation, would hash password
			return nil
		}
	}
	return errors.New("user not found")
}

// UpdateUsedBytes updates the used bytes for a user (mock implementation).
func (m *MockAuthenticator) UpdateUsedBytes(ctx context.Context, userID int64, usedBytes int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, user := range m.users {
		if user.ID == userID {
			user.UsedBytes = usedBytes
			return nil
		}
	}
	return errors.New("user not found")
}

// GetQuotaStatus returns quota information (mock implementation).
func (m *MockAuthenticator) GetQuotaStatus(ctx context.Context, userID int64) (used, quota int64, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, user := range m.users {
		if user.ID == userID {
			return user.UsedBytes, user.QuotaBytes, nil
		}
	}
	return 0, 0, errors.New("user not found")
}

// MockStore provides a mock implementation of a storage store for testing.
type MockStore struct {
	mu      sync.RWMutex
	data    map[string]interface{}
	getErr  error
	setErr  error
	delErr  error
}

// NewMockStore creates a new mock store.
func NewMockStore() *MockStore {
	return &MockStore{
		data: make(map[string]interface{}),
	}
}

// SetGetError sets an error to be returned on Get calls.
func (m *MockStore) SetGetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getErr = err
}

// SetSetError sets an error to be returned on Set calls.
func (m *MockStore) SetSetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setErr = err
}

// SetDeleteError sets an error to be returned on Delete calls.
func (m *MockStore) SetDeleteError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.delErr = err
}

// Get retrieves a value from the mock store.
func (m *MockStore) Get(ctx context.Context, key string) (interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.getErr != nil {
		return nil, m.getErr
	}

	value, exists := m.data[key]
	if !exists {
		return nil, errors.New("key not found")
	}
	return value, nil
}

// Set stores a value in the mock store.
func (m *MockStore) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.setErr != nil {
		return m.setErr
	}

	m.data[key] = value
	return nil
}

// Delete removes a value from the mock store.
func (m *MockStore) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.delErr != nil {
		return m.delErr
	}

	delete(m.data, key)
	return nil
}

// MockQueue provides a mock implementation of a message queue for testing.
type MockQueue struct {
	mu       sync.RWMutex
	messages []interface{}
	enqErr   error
	deqErr   error
}

// NewMockQueue creates a new mock queue.
func NewMockQueue() *MockQueue {
	return &MockQueue{
		messages: make([]interface{}, 0),
	}
}

// SetEnqueueError sets an error to be returned on Enqueue calls.
func (m *MockQueue) SetEnqueueError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enqErr = err
}

// SetDequeueError sets an error to be returned on Dequeue calls.
func (m *MockQueue) SetDequeueError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deqErr = err
}

// Enqueue adds a message to the queue.
func (m *MockQueue) Enqueue(ctx context.Context, message interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.enqErr != nil {
		return m.enqErr
	}

	m.messages = append(m.messages, message)
	return nil
}

// Dequeue retrieves a message from the queue.
func (m *MockQueue) Dequeue(ctx context.Context) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.deqErr != nil {
		return nil, m.deqErr
	}

	if len(m.messages) == 0 {
		return nil, errors.New("queue empty")
	}

	message := m.messages[0]
	m.messages = m.messages[1:]
	return message, nil
}

// Length returns the number of messages in the queue.
func (m *MockQueue) Length(ctx context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.messages), nil
}

// MockDNSResolver provides a mock implementation of a DNS resolver for testing.
type MockDNSResolver struct {
	mu         sync.RWMutex
	mxRecords  map[string][][]string
	txtRecords map[string][]string
	lookupErr  error
}

// NewMockDNSResolver creates a new mock DNS resolver.
func NewMockDNSResolver() *MockDNSResolver {
	return &MockDNSResolver{
		mxRecords:  make(map[string][][]string),
		txtRecords: make(map[string][]string),
	}
}

// SetMXRecords sets MX records for a domain.
func (m *MockDNSResolver) SetMXRecords(domain string, records [][]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mxRecords[domain] = records
}

// SetTXTRecords sets TXT records for a domain.
func (m *MockDNSResolver) SetTXTRecords(domain string, records []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txtRecords[domain] = records
}

// SetLookupError sets an error to be returned on lookup calls.
func (m *MockDNSResolver) SetLookupError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lookupErr = err
}

// LookupMX looks up MX records (mock implementation).
func (m *MockDNSResolver) LookupMX(ctx context.Context, domain string) ([][]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.lookupErr != nil {
		return nil, m.lookupErr
	}

	records, exists := m.mxRecords[domain]
	if !exists {
		return nil, errors.New("MX record not found")
	}
	return records, nil
}

// LookupTXT looks up TXT records (mock implementation).
func (m *MockDNSResolver) LookupTXT(ctx context.Context, domain string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.lookupErr != nil {
		return nil, m.lookupErr
	}

	records, exists := m.txtRecords[domain]
	if !exists {
		return nil, errors.New("TXT record not found")
	}
	return records, nil
}

// MockSMTPServer provides a mock SMTP server for testing.
type MockSMTPServer struct {
	mu       sync.RWMutex
	messages []string
	acceptErr error
}

// NewMockSMTPServer creates a new mock SMTP server.
func NewMockSMTPServer() *MockSMTPServer {
	return &MockSMTPServer{
		messages: make([]string, 0),
	}
}

// SetAcceptError sets an error to be returned on Accept calls.
func (m *MockSMTPServer) SetAcceptError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acceptErr = err
}

// AddMessage adds a received message.
func (m *MockSMTPServer) AddMessage(msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
}

// GetMessages returns all received messages.
func (m *MockSMTPServer) GetMessages() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	messages := make([]string, len(m.messages))
	copy(messages, m.messages)
	return messages
}

// MessageCount returns the number of received messages.
func (m *MockSMTPServer) MessageCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.messages)
}

// ClearMessages clears all received messages.
func (m *MockSMTPServer) ClearMessages() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = make([]string, 0)
}

// MockIMAPServer provides a mock IMAP server for testing.
type MockIMAPServer struct {
	mu        sync.RWMutex
	mailboxes map[string][]string
	flags     map[string]map[string][]string
	selectErr error
}

// NewMockIMAPServer creates a new mock IMAP server.
func NewMockIMAPServer() *MockIMAPServer {
	return &MockIMAPServer{
		mailboxes: make(map[string][]string),
		flags:     make(map[string]map[string][]string),
	}
}

// SetSelectError sets an error to be returned on Select calls.
func (m *MockIMAPServer) SetSelectError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selectErr = err
}

// AddMessage adds a message to a mailbox.
func (m *MockIMAPServer) AddMessage(mailbox string, messageID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.mailboxes[mailbox]; !exists {
		m.mailboxes[mailbox] = make([]string, 0)
		m.flags[mailbox] = make(map[string][]string)
	}
	m.mailboxes[mailbox] = append(m.mailboxes[mailbox], messageID)
	m.flags[mailbox][messageID] = []string{}
}

// GetMessages returns all messages in a mailbox.
func (m *MockIMAPServer) GetMessages(mailbox string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	messages, exists := m.mailboxes[mailbox]
	if !exists {
		return []string{}
	}

	result := make([]string, len(messages))
	copy(result, messages)
	return result
}

// SetFlags sets flags for a message.
func (m *MockIMAPServer) SetFlags(mailbox, messageID string, flags []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.flags[mailbox]; !exists {
		m.flags[mailbox] = make(map[string][]string)
	}
	m.flags[mailbox][messageID] = append([]string{}, flags...)
}

// GetFlags returns flags for a message.
func (m *MockIMAPServer) GetFlags(mailbox, messageID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if mailboxFlags, exists := m.flags[mailbox]; exists {
		if flags, exists := mailboxFlags[messageID]; exists {
			result := make([]string, len(flags))
			copy(result, flags)
			return result
		}
	}
	return []string{}
}

// MessageCount returns the number of messages in a mailbox.
func (m *MockIMAPServer) MessageCount(mailbox string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	messages, exists := m.mailboxes[mailbox]
	if !exists {
		return 0
	}
	return len(messages)
}
