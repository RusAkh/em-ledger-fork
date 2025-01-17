// This software is Copyright (c) 2019-2020 e-Money A/S. It is not offered under an open source license.
//
// Please contact partners@e-money.com for licensing related questions.

// nolint
// autogenerated code using github.com/rigelrozanski/multitool
// aliases generated for the following subdirectories:
// ALIASGEN: github.com/cosmos/cosmos-sdk/x/mint/internal/keeper
// ALIASGEN: github.com/cosmos/cosmos-sdk/x/mint/internal/types
package inflation

import (
	"github.com/e-money/em-ledger/x/inflation/keeper"
	"github.com/e-money/em-ledger/x/inflation/types"
)

const (
	ModuleName        = types.ModuleName
	DefaultParamspace = types.DefaultParamspace
	StoreKey          = types.StoreKey
	QuerierRoute      = types.QuerierRoute
	QueryInflation    = types.QueryInflation
)

var (
	// functions aliases
	NewKeeper              = keeper.NewKeeper
	NewQuerier             = keeper.NewQuerier
	ParamKeyTable          = types.ParamKeyTable
	NewInflationState      = types.NewInflationState
	DefaultInflationState  = types.DefaultInflationState
	ValidateInflationState = types.ValidateInflationState

	// variable aliases
	ModuleCdc = types.ModuleCdc
	MinterKey = types.MinterKey
)

type (
	Keeper          = keeper.Keeper
	InflationState  = types.InflationState
	InflationAsset  = types.InflationAsset
	InflationAssets = types.InflationAssets
	GenesisState    = types.GenesisState
)
