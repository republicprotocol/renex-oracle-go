package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/republicprotocol/renex-oracle-go/types"
	"github.com/rs/cors"
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
	Price float64 `json:"price"`
	Nonce int64   `json:"nonce"`
}

const port int = 3000

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

	go func() {
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
					Price: cmcData.Data.Quotes[pair.FstSymbol].Price,
					Nonce: time.Now().Unix(),
				}
			}
			log.Println(prices)

			// Check every 60 seconds.
			time.Sleep(60 * time.Second) // TODO: Reduce wait time
		}
	}()

	// Handle GET requests to /prices.
	r := mux.NewRouter().StrictSlash(true)
	r.Path("/prices").Queries("fst", "{fstSymbol}", "snd", "{sndSymbol}").Methods("GET").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveResponse(w, r)
	})

	handler := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowCredentials: true,
		AllowedMethods:   []string{"GET"},
	}).Handler(r)

	log.Printf("listening on port %v...", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%v", port), handler); err != nil {
		log.Fatalln(fmt.Sprintf("cannot listen on port %v: %v", port, err))
	}
}

func serveResponse(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		fstSymbol, ok := mux.Vars(r)["fstSymbol"]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("invalid symbol"))
			return
		}
		sndSymbol, ok := mux.Vars(r)["sndSymbol"]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("invalid symbol"))
			return
		}

		// Construct pair object.
		pair := Pair{
			FstSymbol: fstSymbol,
			SndSymbol: sndSymbol,
		}
		midpointData := prices[pair]
		if midpointData.Price == 0 {
			w.WriteHeader(500)
			w.Write([]byte("invalid currency pair"))
			return
		}

		// Respond with price.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		log.Println(midpointData)
		json.NewEncoder(w).Encode(midpointData)
	}
}
