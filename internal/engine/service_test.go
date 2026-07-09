package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"tradenexus/internal/calendar"
	"tradenexus/internal/market"
	"tradenexus/internal/scanner"
	"tradenexus/internal/signals"
)

// --- Mocks ---

type MockCandler struct {
	mock.Mock
}

func (m *MockCandler) GetDaily(ctx context.Context, instrumentID int64) ([]market.Candle, error) {
	args := m.Called(ctx, instrumentID)
	c, _ := args.Get(0).([]market.Candle)
	return c, args.Error(1)
}

func (m *MockCandler) GetAggregates(ctx context.Context, instrumentID int64, tf string) ([]market.Aggregate, error) {
	args := m.Called(ctx, instrumentID, tf)
	a, _ := args.Get(0).([]market.Aggregate)
	return a, args.Error(1)
}

func (m *MockCandler) DailyDateSet(ctx context.Context, instrumentID int64) (map[string]bool, time.Time, time.Time, bool, error) {
	args := m.Called(ctx, instrumentID)
	p, _ := args.Get(0).(map[string]bool)
	t1, _ := args.Get(1).(time.Time)
	t2, _ := args.Get(2).(time.Time)
	return p, t1, t2, args.Bool(3), args.Error(4)
}

func (m *MockCandler) RebuildAggregates(ctx context.Context, instrumentID int64) (int, int, error) {
	args := m.Called(ctx, instrumentID)
	return args.Int(0), args.Int(1), args.Error(2)
}

func (m *MockCandler) UpsertDaily(ctx context.Context, instrumentID int64, candles []market.Candle) (int, error) {
	args := m.Called(ctx, instrumentID, candles)
	return args.Int(0), args.Error(1)
}

func (m *MockCandler) ListInstrumentIDsWithData(ctx context.Context) ([]int64, error) {
	args := m.Called(ctx)
	ids, _ := args.Get(0).([]int64)
	return ids, args.Error(1)
}

type MockRedis struct {
	mock.Mock
}

func (m *MockRedis) IsCachePopulating(ctx context.Context) (bool, error) {
	args := m.Called(ctx)
	return args.Bool(0), args.Error(1)
}

func (m *MockRedis) GetCachedCandles(ctx context.Context, instrumentID int64) ([]byte, error) {
	args := m.Called(ctx, instrumentID)
	b, _ := args.Get(0).([]byte)
	return b, args.Error(1)
}

func (m *MockRedis) SetCachedCandles(ctx context.Context, instrumentID int64, data []byte, ttl time.Duration) error {
	args := m.Called(ctx, instrumentID, data, ttl)
	return args.Error(0)
}

type MockSignaler struct {
	mock.Mock
}

func (m *MockSignaler) Upsert(ctx context.Context, s signals.Signal) (bool, int64, error) {
	args := m.Called(ctx, s)
	return args.Bool(0), int64(args.Int(1)), args.Error(2)
}

func (m *MockSignaler) DeleteOlderThan(ctx context.Context, retention time.Duration) (int64, error) {
	args := m.Called(ctx, retention)
	return int64(args.Int(0)), args.Error(1)
}

type MockCalendarService struct {
	mock.Mock
}

func (m *MockCalendarService) IsMarketOpen(t time.Time) bool {
	args := m.Called(t)
	return args.Bool(0)
}

func (m *MockCalendarService) Cal() *calendar.Calendar {
	return calendar.New(nil)
}


// --- Test Suite ---

type EngineSuite struct {
	suite.Suite
	ctx     context.Context
	candles *MockCandler
	redis   *MockRedis
	signals *MockSignaler
	cal     *MockCalendarService
}

func TestEngineSuite(t *testing.T) {
	suite.Run(t, new(EngineSuite))
}

func (s *EngineSuite) SetupTest() {
	s.ctx = context.Background()
	s.candles = new(MockCandler)
	s.redis = new(MockRedis)
	s.signals = new(MockSignaler)
	s.cal = new(MockCalendarService)
}

func (s *EngineSuite) service() *Service {
	// The real service expects a concrete *calendar.Service, but since we changed
	// the method call inside to be on the service itself, we can pass our mock
	// after casting it. This is a bit of a hack to avoid refactoring the engine's
	// New function to take an interface, which is out of scope.
	type calendarProvider interface {
		IsMarketOpen(time.Time) bool
		Cal() *calendar.Calendar
	}
	var cal calendarProvider = s.cal

	return New(
		s.candles, s.signals, nil, nil, 
		cal.(*MockCalendarService), // This is still not ideal, let's fix New.
		nil, s.redis,
		scanner.DefaultPineConfig(), 30*24*time.Hour, zerolog.Nop(),
	)
}


func (s *EngineSuite) TestScanStored_MarketClosed() {
	instrumentID := int64(123)
	mockCandles := []market.Candle{{Time: time.Now(), Close: 100}}
	mockAggs := []market.Aggregate{}

	s.cal.On("IsMarketOpen", mock.Anything).Return(false)
	s.candles.On("GetDaily", s.ctx, instrumentID).Return(mockCandles, nil).Once()
	s.candles.On("GetAggregates", s.ctx, instrumentID, market.TF1W).Return(mockAggs, nil).Once()
	s.candles.On("GetAggregates", s.ctx, instrumentID, market.TF1M).Return(mockAggs, nil).Once()
	s.signals.On("Upsert", mock.Anything, mock.AnythingOfType("signals.Signal")).Return(false, 0, nil).Maybe()

	// Correctly create the service for this test
	service := New(s.candles, s.signals, nil, nil, s.cal, nil, s.redis, scanner.DefaultPineConfig(), 0, zerolog.Nop())
	_, err := service.ScanStored(s.ctx, instrumentID)

	assert.NoError(s.T(), err)
	s.cal.AssertExpectations(s.T())
	s.candles.AssertExpectations(s.T())
	s.redis.AssertNotCalled(s.T(), "GetCachedCandles", mock.Anything, mock.Anything)
}

func (s *EngineSuite) TestScanStored_MarketOpen_CacheHit() {
	instrumentID := int64(456)
	today := time.Now()
	yesterday := today.AddDate(0, 0, -1)
	
	dbCandles := []market.Candle{{Time: yesterday, Close: 90}}
	todaysCandle := market.Candle{Time: today, Close: 100}
	cachedData, err := json.Marshal([]market.Candle{todaysCandle})
	assert.NoError(s.T(), err)

	s.cal.On("IsMarketOpen", mock.Anything).Return(true)
	s.redis.On("IsCachePopulating", s.ctx).Return(false, nil).Once()
	s.redis.On("GetCachedCandles", s.ctx, instrumentID).Return(cachedData, nil).Once()
	s.candles.On("GetDaily", s.ctx, instrumentID).Return(dbCandles, nil).Once()
	s.signals.On("Upsert", mock.Anything, mock.AnythingOfType("signals.Signal")).Return(false, 0, nil).Maybe()

	service := New(s.candles, s.signals, nil, nil, s.cal, nil, s.redis, scanner.DefaultPineConfig(), 0, zerolog.Nop())
	_, err = service.ScanStored(s.ctx, instrumentID)

	assert.NoError(s.T(), err)
	s.cal.AssertExpectations(s.T())
	s.redis.AssertExpectations(s.T())
	s.candles.AssertExpectations(s.T())
}

func (s *EngineSuite) TestScanStored_MarketOpen_CacheMiss() {
	instrumentID := int64(789)
	yesterday := time.Now().AddDate(0, 0, -1)
	dbCandles := []market.Candle{{Time: yesterday, Close: 90}}

	s.cal.On("IsMarketOpen", mock.Anything).Return(true)
	s.redis.On("IsCachePopulating", s.ctx).Return(false, nil).Once()
	s.redis.On("GetCachedCandles", s.ctx, instrumentID).Return(nil, nil).Once()
	s.candles.On("GetDaily", s.ctx, instrumentID).Return(dbCandles, nil).Once()
	s.signals.On("Upsert", mock.Anything, mock.AnythingOfType("signals.Signal")).Return(false, 0, nil).Maybe()

	service := New(s.candles, s.signals, nil, nil, s.cal, nil, s.redis, scanner.DefaultPineConfig(), 0, zerolog.Nop())
	_, err = service.ScanStored(s.ctx, instrumentID)

	assert.NoError(s.T(), err)
	s.cal.AssertExpectations(s.T())
	s.redis.AssertExpectations(s.T())
	s.candles.AssertExpectations(s.T())
}

func (s *EngineSuite) TestScanStored_WaitsForCache_MarketOpen() {
	instrumentID := int64(999)
	s.cal.On("IsMarketOpen", mock.Anything).Return(true)
	s.redis.On("IsCachePopulating", mock.Anything).Return(true, nil).Once()
	s.redis.On("IsCachePopulating", mock.Anything).Return(false, nil).Once()
	s.redis.On("GetCachedCandles", mock.Anything, instrumentID).Return(nil, nil).Once()
	s.candles.On("GetDaily", mock.Anything, instrumentID).Return(nil, nil).Once()
	s.signals.On("Upsert", mock.Anything, mock.AnythingOfType("signals.Signal")).Return(false, 0, nil).Maybe()

	service := New(s.candles, s.signals, nil, nil, s.cal, nil, s.redis, scanner.DefaultPineConfig(), 0, zerolog.Nop())
	_, err := service.ScanStored(s.ctx, instrumentID)

	assert.NoError(s.T(), err)
	s.redis.AssertExpectations(s.T())
}
