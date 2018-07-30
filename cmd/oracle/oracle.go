package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/republicprotocol/renex-oracle-go/types"
)

type Config struct {
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

type MidpointData struct {
	price float64
	nonce int64
}

var cmcIDs map[string]int32
var prices map[Pair]MidpointData

func main() {
	// Load configuration file containing currency and pair information.
	file, err := ioutil.ReadFile("currencies/currencies.json")
	if err != nil {
		log.Println(fmt.Sprintf("cannot load config file: %v", err))
		return
	}
	var config Config
	if err = json.Unmarshal(file, &config); err != nil {
		log.Println(fmt.Sprintf("cannot unmarshal currency data: %v", err))
		return
	}

	// Store CMC IDs from config file.
	cmcIDs = make(map[string]int32)
	for _, currency := range config.Currencies {
		cmcIDs[currency.Symbol] = currency.ID
	}

	// Retrieve and store price information for each pair within the config file.
	prices = make(map[Pair]MidpointData)
	for {
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
			prices[pair] = MidpointData{
				price: cmcData.Data.Quotes[pair.FstSymbol].Price,
				nonce: time.Now().Unix(),
			}
		}
		log.Println(prices)

		// Check every 5 seconds.
		time.Sleep(5 * time.Second)
	}
}
