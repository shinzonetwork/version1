package cli

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"shinzo/version1/config"
	"shinzo/version1/pkg/defra"
	"shinzo/version1/pkg/logger"

	"github.com/lens-vm/lens/host-go/config/model"
	"github.com/sourcenetwork/immutable"
	"github.com/spf13/cobra"
)

func MakeViewCommand() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "view",
		Short: "Manage views for shinzo",
		Long:  "Manage (create, add, update) views for shinzo",
	}

	return cmd
}

func MakeViewCreateCommand() *cobra.Command {
	cfg, err := config.LoadConfig("config/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	logger.Init(cfg.Logger.Development)
	sugar := logger.Sugar

	var (
		queryFile     string
		sdlFile       string
		transformFile string
	)

	var cmd = &cobra.Command{
		Use:   "create",
		Short: "Create a new view using file inputs",
		Long: `Create a new Shinzo view by passing GraphQL query, SDL, and transform config via files.

Example: add from an argument string:
	shinzo view create --query=query.graphql --sdl=sdl.graphql # without transform
	shinzo view create --query=query.graphql --sdl=sdl.graphql --transform=transform.json # with transform`,
		RunE: func(cmd *cobra.Command, args []string) error {
			queryBytes, err := os.ReadFile(queryFile)
			if err != nil {
				sugar.Errorf("Failed to read query file (%s): %v", queryFile, err)
				return err
			}

			sdlBytes, err := os.ReadFile(sdlFile)
			if err != nil {
				sugar.Errorf("Failed to read SDL file (%s): %v", sdlFile, err)
				return err
			}

			var transform immutable.Option[model.Lens]

			if transformFile != "" {
				transformBytes, err := os.ReadFile(transformFile)
				if err != nil {
					sugar.Errorf("Failed to read transform file (%s): %v", transformFile, err)
					return err
				}

				var lens model.Lens
				if err := json.Unmarshal(transformBytes, &lens); err != nil {
					sugar.Errorf("Invalid JSON in transform file: %v", err)
					return err
				}

				transform = immutable.Some(lens)
				sugar.Info("Transform loaded from file")
			} else {
				transform = immutable.None[model.Lens]()
				sugar.Info("No transform file provided; using None")
			}

			viewHandler := defra.NewViewHandler(cfg.DefraDB.Host, cfg.DefraDB.Port)
			view := viewHandler.CreateView(
				string(queryBytes),
				string(sdlBytes),
				transform,
			)

			// this would be sent to the cosmos blockchain, meaning there should be a transaction to insert this into
			// the blockchain so other nodes can effect it,
			// but for now we just add it to the defraDB

			schemaAndDesc, err := viewHandler.AddView(context.Background(), view, sugar)
			if err != nil {
				sugar.Error(err)
				return err
			}
			sugar.Info(schemaAndDesc)
			return nil
		},
	}

	cmd.Flags().StringVar(&queryFile, "query", "", "Path to GraphQL query file")
	cmd.Flags().StringVar(&sdlFile, "sdl", "", "Path to SDL schema file")
	cmd.Flags().StringVar(&transformFile, "transform", "", "Optional: Path to JSON transform file")

	cmd.MarkFlagRequired("query")
	cmd.MarkFlagRequired("sdl")

	return cmd
}
