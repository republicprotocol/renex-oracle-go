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
	envConfig, err := config.NewConfigFromJSONFile(fmt.Sprintf("env/%v/config.json", network)) // TODO: Retrieve network dynamically
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

				// Send price information to bootstrap nodes.
				request, err := sendPriceToDarknodes(configPair, price, envConfig.BootstrapMultiAddresses, envConfig.Keystore)
				if err != nil {
					log.Println(err)
					continue
				}
				log.Println(fmt.Sprintf("Tokens: %v, Price: %v, Nonce: %v", request.Tokens, request.Price, request.Nonce))
			}

			// Check prices every 5 seconds.
			time.Sleep(5 * time.Second)
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

func sendPriceToDarknodes(configPair types.Pair, price float64, bootstrapMultiAddresses identity.MultiAddresses, keystore crypto.Keystore) (grpc.UpdateMidpointRequest, error) {
	// Construct midpoint price object and sign.
	midpointPrice := oracle.MidpointPrice{
		Tokens: configPair.PairCode,
		Price:  uint64(price * math.Pow10(12)),
		Nonce:  uint64(time.Now().Unix()),
	}
	var err error
	midpointPrice.Signature, err = keystore.EcdsaKey.Sign(midpointPrice.Hash())
	if err != nil {
		return grpc.UpdateMidpointRequest{}, fmt.Errorf("cannot sign midpoint price data: %v", err)
	}

	// Construct request object and send the updated midpoint price to the
	// boostrap nodes.
	request := grpc.UpdateMidpointRequest{
		Signature: midpointPrice.Signature,
		Tokens:    midpointPrice.Tokens,
		Price:     midpointPrice.Price,
		Nonce:     midpointPrice.Nonce,
	}
	dispatch.CoForAll(bootstrapMultiAddresses, func(i int) {
		multiAddr := bootstrapMultiAddresses[i]
		conn, err := grpc.Dial(context.Background(), multiAddr)
		if err != nil {
			log.Println(fmt.Sprintf("cannot dial %v: %v", multiAddr.Address(), err))
			return
		}
		defer conn.Close()
		client := grpc.NewOracleServiceClient(conn)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err = client.UpdateMidpoint(ctx, &request)
		if err != nil {
			log.Println(fmt.Sprintf("cannot update midpoint for %v: %v", multiAddr.Address(), err))
			return
		}
	})

	return request, nil
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
