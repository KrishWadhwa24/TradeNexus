package angel

// loginRequest is the body for loginByPassword.
type loginRequest struct {
	ClientCode string `json:"clientcode"`
	Password   string `json:"password"` // the account PIN
	TOTP       string `json:"totp"`
}

// tokenData holds the three tokens Angel returns on login / refresh.
type tokenData struct {
	JWTToken     string `json:"jwtToken"`
	RefreshToken string `json:"refreshToken"`
	FeedToken    string `json:"feedToken"`
}

// loginResponse is the envelope returned by auth endpoints.
type loginResponse struct {
	Status    bool      `json:"status"`
	Message   string    `json:"message"`
	ErrorCode string    `json:"errorcode"`
	Data      tokenData `json:"data"`
}

// refreshRequest is the body for generateTokens.
type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// histRequest is the body for getCandleData.
type histRequest struct {
	Exchange    string `json:"exchange"`
	SymbolToken string `json:"symboltoken"`
	Interval    string `json:"interval"`
	FromDate    string `json:"fromdate"` // "2006-01-02 15:04"
	ToDate      string `json:"todate"`
}

// histResponse envelope. Data is an array of [ts, o, h, l, c, v] rows.
type histResponse struct {
	Status    bool            `json:"status"`
	Message   string          `json:"message"`
	ErrorCode string          `json:"errorcode"`
	Data      [][]interface{} `json:"data"`
}

// Scrip is one row of the Angel OpenAPI scrip master.
type Scrip struct {
	Token          string `json:"token"`
	Symbol         string `json:"symbol"`
	Name           string `json:"name"`
	Expiry         string `json:"expiry"`
	Strike         string `json:"strike"`
	LotSize        string `json:"lotsize"`
	InstrumentType string `json:"instrumenttype"`
	ExchSeg        string `json:"exch_seg"`
	TickSize       string `json:"tick_size"`
}
