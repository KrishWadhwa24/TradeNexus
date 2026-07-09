package cacher

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"tradenexus/internal/config"
	"tradenexus/internal/market"
)

// --- Mocks ---

type MockAngelClient struct {
	mock.Mock
}

func (m *MockAngelClient) GetDailyCandles(ctx context.Context, exchange, token string, from, to time.Time) ([]market.Candle, error) {
	args := m.Called(ctx, exchange, token, from, to)
	return args.Get(0).([]market.Candle), args.Error(1)
}

type MockRedis struct {
	mock.Mock
}

func (m *MockRedis) SetCachedCandles(ctx context.Context, instrumentID int64, data []byte, ttl time.Duration) error {
	args := m.Called(ctx, instrumentID, data, ttl)
	return args.Error(0)
}

func (m *MockRedis) SetCachePopulating(ctx context.Context, isPopulating bool, ttl time.Duration) error {
	args := m.Called(ctx, isPopulating, ttl)
	return args.Error(0)
}

type MockInstrumentRepo struct {
	mock.Mock
}

func (m *MockInstrumentRepo) GetByID(ctx context.Context, id int64) (market.Instrument, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(market.Instrument), args.Error(1)
}

// --- Tests ---

func TestFetchAndCacheWithRetry_Success(t *testing.T) {
	// --- Arrange ---
	ctx := context.Background()
	instrumentID := int64(123)
	log := zerolog.Nop()
	cfg := config.Config{}

	mockAngel := new(MockAngelClient)
	mockRedis := new(MockRedis)
	mockInst := new(MockInstrumentRepo)

	today := time.Now()
	yesterday := today.AddDate(0, 0, -1)
	todaysCandle := market.Candle{Time: today, Open: 100, High: 110, Low: 90, Close: 105, Volume: 1000}
	yesterdaysCandle := market.Candle{Time: yesterday, Open: 90, High: 100, Low: 80, Close: 95, Volume: 900}
	apiResponse := []market.Candle{yesterdaysCandle, todaysCandle}
	instrument := market.Instrument{ID: instrumentID, Exchange: "NSE", SymbolToken: "FAKE-2024"}

	expectedCacheData, err := json.Marshal([]market.Candle{todaysCandle})
	assert.NoError(t, err)

	mockInst.On("GetByID", ctx, instrumentID).Return(instrument, nil).Once()
	mockAngel.On("GetDailyCandles", ctx, "NSE", "FAKE-2024", mock.Anything, mock.Anything).Return(apiResponse, nil).Once()
	mockRedis.On("SetCachedCandles", ctx, instrumentID, expectedCacheData, mock.Anything).Return(nil).Once()

	sut := New(cfg, log, mockInst, nil, mockAngel, mockRedis, nil)

	// --- Act ---
	sut.fetchAndCacheWithRetry(ctx, instrumentID)

	// --- Assert ---
	mockAngel.AssertExpectations(t)
	mockRedis.AssertExpectations(t)
	mockInst.AssertExpectations(t)
}

func TestFetchAndCacheWithRetry_NoTodayCandle(t *testing.T) {
	// --- Arrange ---
	ctx := context.Background()
	instrumentID := int64(123)
	log := zerolog.Nop()
	cfg := config.Config{}

	mockAngel := new(MockAngelClient)
	mockRedis := new(MockRedis)
	mockInst := new(MockInstrumentRepo)

	yesterday := time.Now().AddDate(0, 0, -1)
	apiResponse := []market.Candle{{Time: yesterday}}
	instrument := market.Instrument{ID: instrumentID, Exchange: "NSE", SymbolToken: "FAKE-2024"}

	mockInst.On("GetByID", ctx, instrumentID).Return(instrument, nil).Once()
	mockAngel.On("GetDailyCandles", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(apiResponse, nil).Once()

	sut := New(cfg, log, mockInst, nil, mockAngel, mockRedis, nil)

	// --- Act ---
	sut.fetchAndCacheWithRetry(ctx, instrumentID)

	// --- Assert ---
	mockAngel.AssertExpectations(t)
	mockRedis.AssertNotCalled(t, "SetCachedCandles", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockInst.AssertExpectations(t)
}

type MockMarketStatusChecker struct {
	mock.Mock
}

func (m *MockMarketStatusChecker) IsMarketOpen(t time.Time) bool {
	args := m.Called(t)
	return args.Bool(0)
}

type MockCandleRepo struct {
	mock.Mock
}

func (m *MockCandleRepo) ListInstrumentIDsWithData(ctx context.Context) ([]int64, error) {
	args := m.Called(ctx)
	return args.Get(0).([]int64), args.Error(1)
}

func TestStart_MarketOpen(t *testing.T) {
	// --- Arrange ---
	log := zerolog.Nop()
	mockCal := new(MockMarketStatusChecker)
	mockCandles := new(MockCandleRepo)
	mockRedis := new(MockRedis)

	cfg := config.Config{CacheEnabled: true, CacheInterval: 10 * time.Millisecond}

	mockCal.On("IsMarketOpen", mock.Anything).Return(true)
	mockCandles.On("ListInstrumentIDsWithData", mock.Anything).Return([]int64{}, nil).Maybe()
	mockRedis.On("SetCachePopulating", mock.Anything, mock.Anything).Return(nil).Maybe()

	sut := New(cfg, log, nil, mockCandles, nil, mockRedis, mockCal)

	// --- Act ---
	sut.Start()
	time.Sleep(15 * time.Millisecond) 
	sut.Stop()
	time.Sleep(5 * time.Millisecond)

	// --- Assert ---
	mockCal.AssertCalled(t, "IsMarketOpen", mock.Anything)
	mockCandles.AssertCalled(t, "ListInstrumentIDsWithData", mock.Anything)
}

func TestStart_MarketClosed(t *testing.T) {
	// --- Arrange ---
	log := zerolog.Nop()
	mockCal := new(MockMarketStatusChecker)
	mockCandles := new(MockCandleRepo)

	cfg := config.Config{CacheEnabled: true, CacheInterval: 10 * time.Millisecond}

	mockCal.On("IsMarketOpen", mock.Anything).Return(false)

	sut := New(cfg, log, nil, mockCandles, nil, nil, mockCal)

	// --- Act ---
	sut.Start()
	time.Sleep(15 * time.Millisecond)
	sut.Stop()
	time.Sleep(5 * time.Millisecond)

	// --- Assert ---
	mockCal.AssertCalled(t, "IsMarketOpen", mock.Anything)
	mockCandles.AssertNotCalled(t, "ListInstrumentIDsWithData", mock.Anything)
}
