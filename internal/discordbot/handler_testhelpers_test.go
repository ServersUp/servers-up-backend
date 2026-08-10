package discordbot

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/ServersUp/servers-up-backend/internal/servermap"
	"github.com/aws/aws-lambda-go/events"
)

// MockDatabase implements the Database interface for testing.
type MockDatabase struct {
	AddFunc        func(ctx context.Context, sub models.Subscription) error
	DeleteFunc     func(ctx context.Context, guildID, channelID, serverID, subscriptionID string) error
	ListFunc       func(ctx context.Context, guildID string) ([]models.Subscription, error)
	ListByServerFn func(ctx context.Context, serverID string) ([]models.Subscription, error)
}

func (m *MockDatabase) AddSubscription(ctx context.Context, sub models.Subscription) error {
	return m.AddFunc(ctx, sub)
}
func (m *MockDatabase) DeleteSubscription(ctx context.Context, guildID, channelID, serverID, subscriptionID string) error {
	return m.DeleteFunc(ctx, guildID, channelID, serverID, subscriptionID)
}
func (m *MockDatabase) ListSubscriptionsByGuild(ctx context.Context, guildID string) ([]models.Subscription, error) {
	return m.ListFunc(ctx, guildID)
}
func (m *MockDatabase) ListSubscriptionsByServer(ctx context.Context, serverID string) ([]models.Subscription, error) {
	if m.ListByServerFn != nil {
		return m.ListByServerFn(ctx, serverID)
	}
	return nil, nil
}

// MockConfig implements the ConfigProvider interface for testing.
type MockConfig struct {
	LoadFunc func(ctx context.Context, bucket, key string, target any) error
}

func (m *MockConfig) LoadJSONFromS3(ctx context.Context, bucket, key string, target any) error {
	return m.LoadFunc(ctx, bucket, key, target)
}

// MockStatusStore implements StatusStore for testing.
type MockStatusStore struct {
	GetFunc func(ctx context.Context, gameID, serverID string) (*models.GameServerStatus, error)
}

func (m *MockStatusStore) GetServerStatus(ctx context.Context, gameID, serverID string) (*models.GameServerStatus, error) {
	return m.GetFunc(ctx, gameID, serverID)
}

// testMockConfig returns a MockConfig with the standard multi-region test mapping.
func testMockConfig() *MockConfig {
	return &MockConfig{
		LoadFunc: func(ctx context.Context, bucket, key string, target any) error {
			m := target.(*servermap.Mapping)
			m.Games = map[string]servermap.Game{
				"wow": {
					Provider: "battlenet",
					Regions: map[string]servermap.Region{
						"us": {Servers: map[string]servermap.Server{
							"illidan": {Identifier: 57},
						}},
					},
				},
				"wipe": {
					Provider: "other",
					Regions: map[string]servermap.Region{
						"us": {Servers: map[string]servermap.Server{
							"alpha": {Identifier: 1},
						}},
					},
				},
			}
			return nil
		},
	}
}

// testHandlerFixture bundles a Handler and signing keys for handler-level tests.
type testHandlerFixture struct {
	handler *Handler
	priv    ed25519.PrivateKey
	db      *MockDatabase
	config  *MockConfig
	scopes  *MockScopeStateStore
	status  *MockGameStatusLister
}

// MockScopeStateStore implements the scopeStateStore interface for testing.
type MockScopeStateStore struct {
	states      map[string]models.ScopeState
	PutCalls    []models.ScopeState
	DeleteCalls []string
}

func (m *MockScopeStateStore) Get(_ context.Context, key string) (*models.ScopeState, error) {
	if m.states == nil {
		return nil, nil
	}
	st, ok := m.states[key]
	if !ok {
		return nil, nil
	}
	return &st, nil
}

func (m *MockScopeStateStore) Put(_ context.Context, st models.ScopeState) error {
	if m.states == nil {
		m.states = map[string]models.ScopeState{}
	}
	m.PutCalls = append(m.PutCalls, st)
	m.states[st.ScopeKey] = st
	return nil
}

func (m *MockScopeStateStore) Delete(_ context.Context, key string) error {
	m.DeleteCalls = append(m.DeleteCalls, key)
	return nil
}

// MockGameStatusLister implements the gameStatusLister interface for testing.
type MockGameStatusLister struct {
	ByGame map[string][]models.GameServerStatus
}

func (m *MockGameStatusLister) ListServerStatusesByGame(_ context.Context, gameID string) ([]models.GameServerStatus, error) {
	return m.ByGame[gameID], nil
}

func newTestHandlerFixture(t *testing.T) *testHandlerFixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	mockDB := &MockDatabase{}
	mockCfg := testMockConfig()
	scopes := &MockScopeStateStore{}
	h := &Handler{
		database:         mockDB,
		configProvider:   mockCfg,
		discordPublicKey: hex.EncodeToString(pub),
		mappingCache:     servermap.NewCachedMapping(0),
		scopeStates:      scopes,
	}
	return &testHandlerFixture{
		handler: h,
		priv:    priv,
		db:      mockDB,
		config:  mockCfg,
		scopes:  scopes,
	}
}

// newStatusHandlerFixture creates a fixture with a StatusStore and rate-limiter.
func newStatusHandlerFixture(t *testing.T, store *MockStatusStore) *testHandlerFixture {
	t.Helper()
	f := newTestHandlerFixture(t)
	f.handler.statusStore = store
	f.handler.statusLimiter = newStatusRateLimiter()
	f.handler.statusCache = newStatusResultCache()
	return f
}

// signedRequest signs body with the fixture key and returns a ready LambdaFunctionURLRequest.
func (f *testHandlerFixture) signedRequest(t *testing.T, body string) events.LambdaFunctionURLRequest {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := hex.EncodeToString(ed25519.Sign(f.priv, []byte(ts+body)))
	return events.LambdaFunctionURLRequest{
		Headers: map[string]string{
			"x-signature-ed25519":   sig,
			"x-signature-timestamp": ts,
		},
		Body: body,
	}
}
