package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/republicprotocol/renex-oracle-go/types"
)

type Currency struct {
	ID       int32   `json:"id"`
	symbol   string  `json:"symbol"`
	ethPrice float64 `json:"ethPrice"`
}

var currencies []Currency

func main() {
	// Load configuration file containing CoinMarketCap currency IDs.
	file, err := ioutil.ReadFile("currencies/currencies.json")
	if err != nil {
		log.Println(fmt.Sprintf("cannot load config file: %v", err))
		return
	}
	if err = json.Unmarshal(file, &currencies); err != nil {
		log.Println(fmt.Sprintf("cannot unmarshal currency data: %v", err))
		return
	}

	for i, currency := range currencies {
		res, err := http.DefaultClient.Get(fmt.Sprintf("https://api.coinmarketcap.com/v2/ticker/%d/?convert=ETH", currency.ID))
		if err != nil {
			log.Println(fmt.Sprintf("cannot get price information for %s: %v", currency.symbol, err))
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
		currencies[i].ethPrice = cmcData.Data.Quotes["ETH"].Price
	}

	log.Println(currencies)
}
