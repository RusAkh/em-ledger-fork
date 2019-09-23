package cli

import (
	"emoney/x/issuer/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/context"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/auth/client/utils"
	"github.com/spf13/cobra"
)

func GetTxCmd(cdc *codec.Codec) *cobra.Command {
	issuanceTxCmd := &cobra.Command{
		Use:                        "issuer",
		Aliases:                    []string{"i"},
		DisableFlagParsing:         false,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	issuanceTxCmd.AddCommand(
		client.PostCommands(
			getCmdIncreaseCredit(cdc),
			getCmdDecreaseCredit(cdc),
		)...,
	)

	return issuanceTxCmd
}

func getCmdIncreaseCredit(cdc *codec.Codec) *cobra.Command {
	return &cobra.Command{
		Use:   "increase-credit [issuer_key_or_address] [liquidity_provider_address] [amount]",
		Short: "Increase the credit of a liquidity provider.",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			txBldr := auth.NewTxBuilderFromCLI().WithTxEncoder(utils.GetTxEncoder(cdc))
			cliCtx := context.NewCLIContextWithFrom(args[0]).WithCodec(cdc)

			lpAcc, err := sdk.AccAddressFromBech32(args[1])
			if err != nil {
				return err
			}

			creditIncrease, err := sdk.ParseCoins(args[2])
			if err != nil {
				return err
			}

			msg := types.MsgIncreaseCredit{
				CreditIncrease:    creditIncrease,
				LiquidityProvider: lpAcc,
				Issuer:            cliCtx.GetFromAddress(),
			}

			return utils.GenerateOrBroadcastMsgs(cliCtx, txBldr, []sdk.Msg{msg})
		},
	}
}

func getCmdDecreaseCredit(cdc *codec.Codec) *cobra.Command {
	return &cobra.Command{
		Use:   "decrease-credit [issuer_key_or_address] [liquidity_provider_address] [amount]",
		Short: "Decrease the credit of a liquidity provider. Credit cannot be negative",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			txBldr := auth.NewTxBuilderFromCLI().WithTxEncoder(utils.GetTxEncoder(cdc))
			cliCtx := context.NewCLIContextWithFrom(args[0]).WithCodec(cdc)

			lpAcc, err := sdk.AccAddressFromBech32(args[1])
			if err != nil {
				return err
			}

			creditDecrease, err := sdk.ParseCoins(args[2])
			if err != nil {
				return err
			}

			msg := types.MsgDecreaseCredit{
				CreditDecrease:    creditDecrease,
				LiquidityProvider: lpAcc,
				Issuer:            cliCtx.GetFromAddress(),
			}

			return utils.GenerateOrBroadcastMsgs(cliCtx, txBldr, []sdk.Msg{msg})
		},
	}
}
