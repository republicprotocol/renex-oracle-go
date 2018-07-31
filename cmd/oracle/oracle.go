package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/republicprotocol/renex-oracle-go/types"
	"github.com/republicprotocol/republic-go/contract"
	"github.com/republicprotocol/republic-go/grpc"
	"github.com/republicprotocol/republic-go/identity"
	"github.com/rs/cors"
)

type config struct {
	env        envConfig
	currencies currenciesConfig
}

type envConfig struct {
	Ethereum                contract.Config         `json:"ethereum"`
	BootstrapMultiAddresses identity.MultiAddresses `json:"bootstrapMultiAddresses"`
}

type currenciesConfig struct {
	Currencies []currency `json:"currencies"`
	Pairs      []pair     `json:"pairs"`
}

type currency struct {
	Symbol string `json:"symbol"`
	ID     int32  `json:"id"`
}

type pair struct {
	FstSymbol string `json:"fstSymbol"`
	SndSymbol string `json:"sndSymbol"`
}

type midpointData struct {
	Price uint64 `json:"price"`
	Nonce uint64 `json:"nonce"`
}

const port int = 3000

var cmcIDs map[string]int32
var prices map[pair]midpointData

func main() {
	config := config{}

	// Load configuration file containing currency and pair information.
	file, err := ioutil.ReadFile("currencies/currencies.json")
	if err != nil {
		log.Println(fmt.Sprintf("cannot load config file: %v", err))
		return
	}
	if err = json.Unmarshal(file, &config.currencies); err != nil {
		log.Println(fmt.Sprintf("cannot unmarshal config file: %v", err))
		return
	}

	// Load configuration file containing environment information.
	file, err = ioutil.ReadFile(fmt.Sprintf("env/%v/config.json", "nightly")) // TODO: Retrieve network dynamically
	if err != nil {
		log.Println(fmt.Sprintf("cannot load config file: %v", err))
		return
	}
	if err = json.Unmarshal(file, &config.env); err != nil {
		log.Println(fmt.Sprintf("cannot unmarshal config file: %v", err))
		return
	}

	// Store CMC IDs from config file.
	cmcIDs = make(map[string]int32)
	for _, currency := range config.currencies.Currencies {
		cmcIDs[currency.Symbol] = currency.ID
	}

	// Retrieve price information for each pair within the config file and
	// propogate to clients.
	prices = make(map[pair]midpointData)
	client := grpc.NewOracleClient([]byte{})
	go func() {
		for {
			for _, pair := range config.currencies.Pairs {
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
				prices[pair] = midpointData{
					Price: uint64(cmcData.Data.Quotes[pair.FstSymbol].Price), // TODO: Fix price conversion
					Nonce: uint64(time.Now().Unix()),
				}

				// Update the midpoint price for the boostrap nodes.
				for _, multiAddr := range config.env.BootstrapMultiAddresses {
					client.UpdateMidpoint(context.Background(), multiAddr, 0, prices[pair].Price, prices[pair].Nonce)
				}
			}

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
		log.Println(fmt.Sprintf("cannot listen on port %v: %v", port, err))
		return
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
		pair := pair{
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
