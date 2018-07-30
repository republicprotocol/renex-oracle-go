package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/republicprotocol/renex-oracle-go/types"
)

type CurrenciesConfig struct {
	Currencies []Currency `json:"currencies"`
	Pairs      []Pair     `json:"pairs"`
}

type Currency struct {
	Symbol string `json:"symbol"`
	ID     int32  `json:"id"`
}

type Pair struct {
	FstSymbol string `json:"fstSymbol"`
	SndSymbol string `json:"sndSymbol"`
}

var cmcIDs map[string]int32
var prices map[Pair]float64

func main() {
	// Load configuration file containing CoinMarketCap currency IDs.
	file, err := ioutil.ReadFile("currencies/currencies.json")
	if err != nil {
		log.Println(fmt.Sprintf("cannot load config file: %v", err))
		return
	}
	var config CurrenciesConfig
	if err = json.Unmarshal(file, &config); err != nil {
		log.Println(fmt.Sprintf("cannot unmarshal currency data: %v", err))
		return
	}

	cmcIDs = make(map[string]int32)
	for _, currency := range config.Currencies {
		cmcIDs[currency.Symbol] = currency.ID
	}

	prices = make(map[Pair]float64)
	for _, pair := range config.Pairs {
		res, err := http.DefaultClient.Get(fmt.Sprintf("https://api.coinmarketcap.com/v2/ticker/%d/?convert=%s", cmcIDs[pair.SndSymbol], pair.FstSymbol))
		if err != nil {
			log.Println(fmt.Sprintf("cannot get price information for pair [%s, %s]: %v", pair.FstSymbol, pair.SndSymbol, err))
			continue
		}
		defer res.Body.Close()

		cmcDataBytes, err := ioutil.ReadAll(res.Body)
		if err != nil {
			log.Println(fmt.Sprintf("cannot read response: %v", err))
			continue
		}

		var cmcData types.TickerResponse
		if err = json.Unmarshal(cmcDataBytes, &cmcData); err != nil {
			log.Println(fmt.Sprintf("cannot unmarshal response: %v", err))
			continue
		}
		prices[pair] = cmcData.Data.Quotes[pair.FstSymbol].Price
	}

	log.Println(prices)
}
