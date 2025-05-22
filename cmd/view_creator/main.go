package main

import (
	"context"
	"log"
	"path"
	"runtime"
	"shinzo/version1/config"
	"shinzo/version1/pkg/defra"
	"shinzo/version1/pkg/logger"

	"github.com/lens-vm/lens/host-go/config/model"
	"github.com/sourcenetwork/immutable"
)

func main() {
	cfg, err := config.LoadConfig("config/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	logger.Init(cfg.Logger.Development)
	sugar := logger.Sugar

	// queries
	// types

	// lenses parameters (abi, address, ...)

	viewHandler := defra.NewViewHandler(cfg.DefraDB.Host, cfg.DefraDB.Port)
	// create view for test purposes, in real prod enviroment we get the data from either user input or cosmos blocks parsing
	view := viewHandler.CreateView(
		`
			Transaction {
				from
				to
			}
		`,
		`
			type TxTestView3 @materialized(if: false) {
				from: String,
				to: String,
			}
		`,
		// /***
		// logs(filter: {topics: {_any: {}}}) {
		// 	address
		// 	topics
		// 	events {
		// 		eventName
		// 	}
		// }
		// DecodedView
		// 		// address: String,
		// eventName: String,
		// topic0: String,
		// topic1: String,
		// topic2: String,
		// topic3: String,
		// topic4: String
		//
		//
		// /
		// immutable.None[model.Lens](),
		immutable.Some(model.Lens{
			// This transform will copy the value from `firstName` into the `fullName` field,
			// like an overly-complicated alias
			Lenses: []model.LensModule{
				{
					Path: getPathRelativeToProjectRoot("lenses/rust_wasm32_combine_view/target/wasm32-unknown-unknown/debug/rust_wasm32_combine_view.wasm"), // path to the lenses file, this will be created in the /lenses folder
					// Arguments: map[string]any{
					// 	"abi": "{...}",
					// },
				},
			},
		}),
	)

	schemaAndDesc, err := viewHandler.AddView(context.Background(), view, sugar)
	if err != nil {
		sugar.Error(err)
		return
	}
	sugar.Info(schemaAndDesc)
}

func getPathRelativeToProjectRoot(relativePath string) string {
	_, filename, _, _ := runtime.Caller(0)
	root := path.Dir(path.Dir(path.Dir(filename)))
	return path.Join(root, relativePath)
}
