# RenEx Oracle

A centralised RenEx service used to provide data about the current midpoint price.

## Description

The RenEx Oracle retrieves pricing information from the CoinMarketCap API and sends this information to the Bootstrap nodes, which in turn propagate this information throughout the rest of the network. The prices are then used by the Darknodes for determining pricing information for midpoint orders in the RenEx dark pool.

## Configuration

The supported currencies are defined in `currencies/currencies.json`. To add additional currencies, the symbol name, CoinMarketCap ID, as well as valid pairs must be defined. The Oracle will use the multi-addresses defined for the Bootstrap nodes in `env/<network>/config.json`. Currently, only the `nightly` network is supported.