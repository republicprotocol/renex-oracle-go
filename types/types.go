package types

type Config struct {
	Currencies []Currency `json:"currencies"`
	Pairs      []Pair     `json:"pairs"`
}

type Currency struct {
	Symbol string `json:"symbol"`
	CmcID  int32  `json:"cmcID"`
}

type Pair struct {
	FstSymbol string `json:"fstSymbol"`
	SndSymbol string `json:"sndSymbol"`
	PairCode  uint64 `json:"pairCode"`
}

// CMC types adapted from https://github.com/CoinCircle/go-coinmarketcap/blob/master/types/types.go
type TickerResponse struct {
	Data     Ticker `json:"data"`
	Metadata struct {
		Timestamp int64
		Error     string `json:"error"`
	}
}

type Ticker struct {
	ID                int                     `json:"id"`
	Name              string                  `json:"name"`
	Symbol            string                  `json:"symbol"`
	Slug              string                  `json:"website_slug"`
	Rank              int                     `json:"rank"`
	CirculatingSupply float64                 `json:"circulating_supply"`
	TotalSupply       float64                 `json:"total_supply"`
	MaxSupply         float64                 `json:"max_supply"`
	Quotes            map[string]*TickerQuote `json:"quotes"`
	LastUpdated       int                     `json:"last_updated"`
}

type TickerQuote struct {
	Price            float64 `json:"price"`
	Volume24H        float64 `json:"volume_24h"`
	MarketCap        float64 `json:"market_cap"`
	PercentChange1H  float64 `json:"percent_change_1h"`
	PercentChange24H float64 `json:"percent_change_24h"`
	PercentChange7D  float64 `json:"percent_change_7d"`
}
