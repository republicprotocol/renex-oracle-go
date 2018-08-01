package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/republicprotocol/republic-go/dispatch"

	"github.com/gorilla/mux"
	"github.com/republicprotocol/renex-oracle-go/types"
	"github.com/republicprotocol/republic-go/cmd/darknode/config"
	"github.com/republicprotocol/republic-go/grpc"
	"github.com/republicprotocol/republic-go/oracle"
	"github.com/rs/cors"
)

type CurrencyPair struct {
	fstSymbol string
	sndSymbol string
}

const port int = 3000

var cmcIDs map[string]int32
var prices map[CurrencyPair]float64

func main() {
	var currenciesConfig types.Config

	// Load configuration file containing currency and pair information.
	file, err := ioutil.ReadFile("currencies/currencies.json")
	if err != nil {
		log.Fatalln(fmt.Sprintf("cannot load config file: %v", err))
	}
	if err = json.Unmarshal(file, &currenciesConfig); err != nil {
		log.Fatalln(fmt.Sprintf("cannot unmarshal config file: %v", err))
	}

	// Load configuration file containing environment information.
	envConfig, err := config.NewConfigFromJSONFile(fmt.Sprintf("env/%v/config.json", "nightly")) // TODO: Retrieve network dynamically
	if err != nil {
		log.Fatalln(fmt.Sprintf("cannot load config file: %v", err))
	}

	// Store CMC IDs from config file.
	cmcIDs = make(map[string]int32)
	for _, currency := range currenciesConfig.Currencies {
		cmcIDs[currency.Symbol] = currency.ID
	}

	// Retrieve price information for each pair within the config file and
	// propogate to clients.
	prices = make(map[CurrencyPair]float64)
	go func() {
		for {
			for _, configPair := range currenciesConfig.Pairs {
				res, err := http.DefaultClient.Get(fmt.Sprintf("https://api.coinmarketcap.com/v2/ticker/%d/?convert=%s", cmcIDs[configPair.SndSymbol], configPair.FstSymbol))
				if err != nil {
					log.Println(fmt.Sprintf("cannot get price information for pair [%s, %s]: %v", configPair.FstSymbol, configPair.SndSymbol, err))
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

				// Store price for GET requests.
				pair := CurrencyPair{
					fstSymbol: configPair.FstSymbol,
					sndSymbol: configPair.SndSymbol,
				}
				price := cmcData.Data.Quotes[configPair.FstSymbol].Price
				prices[pair] = price

				// Construct midpoint price object and sign.
				midpointPrice := oracle.MidpointPrice{
					Tokens: configPair.PairCode,
					Price:  uint64(price * math.Pow10(12)),
					Nonce:  uint64(time.Now().Unix()),
				}
				midpointPrice.Signature, err = envConfig.Keystore.EcdsaKey.Sign(midpointPrice.Hash())
				if err != nil {
					log.Println(fmt.Sprintf("cannot sign midpoint price data: %v", err))
					continue
				}

				// Update the midpoint price for the boostrap nodes.
				dispatch.CoForAll(envConfig.BootstrapMultiAddresses, func(i int) {
					multiAddr := envConfig.BootstrapMultiAddresses[i]
					conn, err := grpc.Dial(context.Background(), multiAddr)
					if err != nil {
						log.Println(fmt.Sprintf("cannot dial %v: %v", multiAddr.Address(), err))
						return
					}
					defer conn.Close()
					client := grpc.NewOracleServiceClient(conn)

					request := grpc.UpdateMidpointRequest{
						Signature: midpointPrice.Signature,
						Tokens:    midpointPrice.Tokens,
						Price:     midpointPrice.Price,
						Nonce:     midpointPrice.Nonce,
					}

					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()

					_, err = client.UpdateMidpoint(ctx, &request)
					if err != nil {
						log.Println(fmt.Sprintf("cannot update midpoint for %v: %v", multiAddr.Address(), err))
						return
					}
				})
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
		pair := CurrencyPair{
			fstSymbol: fstSymbol,
			sndSymbol: sndSymbol,
		}
		midpointPrice := prices[pair]
		if midpointPrice == 0 {
			w.WriteHeader(500)
			w.Write([]byte("invalid currency pair"))
			return
		}

		// Respond with price.
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("%f", midpointPrice)))
	}
}
