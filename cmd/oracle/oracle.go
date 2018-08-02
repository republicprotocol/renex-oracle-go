package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/republicprotocol/republic-go/crypto"
	"github.com/republicprotocol/republic-go/dispatch"
	"github.com/republicprotocol/republic-go/identity"

	"github.com/gorilla/mux"
	"github.com/republicprotocol/renex-oracle-go/types"
	"github.com/republicprotocol/republic-go/cmd/darknode/config"
	"github.com/republicprotocol/republic-go/grpc"
	"github.com/republicprotocol/republic-go/oracle"
	"github.com/rs/cors"
)

type currencyPair struct {
	fstSymbol string
	sndSymbol string
}

var (
	cmcIDs map[string]int32         // Map of a token symbol to its CMC ID
	prices map[currencyPair]float64 // Map of a currency pair to its latest price
)

func main() {
	cmcIDs = make(map[string]int32)
	prices = make(map[currencyPair]float64)

	// Load environment variables.
	port := os.Getenv("PORT")
	updateInterval := os.Getenv("INTERVAL") // The interval at which prices are retrieved from the CMC API.
	network := os.Getenv("NETWORK")
	if network == "" {
		log.Fatalln("cannot read network environment")
	}

	// Load configuration file containing currency and pair information.
	file, err := ioutil.ReadFile("currencies/currencies.json")
	if err != nil {
		log.Fatalln(fmt.Sprintf("cannot load config file: %v", err))
	}
	var currenciesConfig types.Config
	if err = json.Unmarshal(file, &currenciesConfig); err != nil {
		log.Fatalln(fmt.Sprintf("cannot unmarshal config file: %v", err))
	}

	// Load configuration file containing environment information.
	envConfig, err := config.NewConfigFromJSONFile(fmt.Sprintf("env/%v/config.json", network))
	if err != nil {
		log.Fatalln(fmt.Sprintf("cannot load config file: %v", err))
	}

	// Store CMC IDs from config file.
	for _, currency := range currenciesConfig.Currencies {
		cmcIDs[currency.Symbol] = currency.CmcID
	}

	// Retrieve price information for each pair within the config file and
	// propogate to clients.
	go func() {
		for {
			for _, configPair := range currenciesConfig.Pairs {
				// Retrieve price information for a given token.
				price, err := retrievePrice(configPair.FstSymbol, configPair.SndSymbol)
				if err != nil {
					log.Println(err)
					continue
				}

				// Store price for GET requests.
				pair := currencyPair{
					fstSymbol: configPair.FstSymbol,
					sndSymbol: configPair.SndSymbol,
				}
				prices[pair] = price
			}

			// Send price information to bootstrap nodes.
			request, err := sendPricesToDarknodes(currenciesConfig.Pairs, envConfig.BootstrapMultiAddresses, envConfig.Keystore)
			if err != nil {
				log.Println(err)
				return
			}
			log.Println(fmt.Sprintf("TokenPairs: %v, Prices: %v, Nonce: %v", request.TokenPairs, request.Prices, request.Nonce))

			// Check prices on interval specified using the environment
			// variable (default: 10s).
			var interval int
			interval, err = strconv.Atoi(updateInterval)
			if err != nil {
				interval = 10
			}
			time.Sleep(time.Duration(interval) * time.Second)
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

func retrievePrice(fstSymbol, sndSymbol string) (float64, error) {
	// Retrieve the price for sndSymbol with respect to fstSymbol.
	res, err := http.DefaultClient.Get(fmt.Sprintf("https://api.coinmarketcap.com/v2/ticker/%d/?convert=%s", cmcIDs[sndSymbol], fstSymbol))
	if err != nil {
		return 0, fmt.Errorf("cannot get price information for pair [%s, %s]: %v", fstSymbol, sndSymbol, err)
	}
	defer res.Body.Close()

	cmcDataBytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return 0, fmt.Errorf("cannot read response: %v", err)
	}

	var cmcData types.TickerResponse
	if err = json.Unmarshal(cmcDataBytes, &cmcData); err != nil {
		return 0, fmt.Errorf("cannot unmarshal response: %v", err)
	}

	return cmcData.Data.Quotes[fstSymbol].Price, nil
}

func sendPricesToDarknodes(pairs []types.Pair, bootstrapMultiAddresses identity.MultiAddresses, keystore crypto.Keystore) (oracle.MidpointPrice, error) {
	// Formatting data for request.
	var tokenPairs []uint64
	for _, pair := range pairs {
		tokenPairs = append(tokenPairs, pair.PairCode)
	}

	var pairPrices []uint64
	for _, price := range prices {
		pairPrices = append(pairPrices, uint64(price*math.Pow10(12)))
	}

	// Construct midpoint price object and sign.
	var err error
	midpointPrice := oracle.MidpointPrice{
		TokenPairs: tokenPairs,
		Prices:     pairPrices,
		Nonce:      uint64(time.Now().Unix()),
	}
	midpointPrice.Signature, err = keystore.EcdsaKey.Sign(midpointPrice.Hash())
	if err != nil {
		return oracle.MidpointPrice{}, fmt.Errorf("cannot sign midpoint price data: %v", err)
	}

	// Send the updated midpoint price to the boostrap nodes.
	dispatch.CoForAll(bootstrapMultiAddresses, func(i int) {
		multiAddr := bootstrapMultiAddresses[i]
		client := grpc.NewOracleClient("", nil)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = client.UpdateMidpoint(ctx, multiAddr, midpointPrice)
		if err != nil {
			log.Println(fmt.Sprintf("cannot update midpoint for %v: %v", multiAddr.Address(), err))
			return
		}
	})

	return midpointPrice, nil
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

		// Construct pair object and retrieve price.
		pair := currencyPair{
			fstSymbol: fstSymbol,
			sndSymbol: sndSymbol,
		}
		midpointPrice := prices[pair]

		// Respond with price.
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("%f", midpointPrice)))
	}
}
