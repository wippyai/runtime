// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wippyai/runtime/boot/deps/artifact"
	"github.com/wippyai/wapp"
)

var artifactsCmd = &cobra.Command{
	Use:   "artifacts",
	Short: "Work with build-time filesystem artifacts",
}

var artifactsMaterializeCmd = &cobra.Command{
	Use:   "materialize <pack.wapp> <namespace:name>",
	Short: "Validate and materialize an embedded artifact resource",
	Long: `Materialize one artifact filesystem from an existing WAPP.

The resource must declare meta.artifact.format and the format must be registered
in this CLI. The command does not resolve module dependencies, mutate
wippy.lock, invoke package managers, or participate in runtime composition.`,
	Args: cobra.ExactArgs(2),
	RunE: runArtifactsMaterialize,
}

func init() {
	rootCmd.AddCommand(artifactsCmd)
	artifactsCmd.AddCommand(artifactsMaterializeCmd)
	artifactsMaterializeCmd.Flags().String("root", ".wippy", "materialization root")
}

func runArtifactsMaterialize(cmd *cobra.Command, args []string) error {
	root, err := cmd.Flags().GetString("root")
	if err != nil {
		return fmt.Errorf("read artifact root: %w", err)
	}
	resourceID, err := parseArtifactResourceID(args[1])
	if err != nil {
		return err
	}

	file, err := os.Open(args[0])
	if err != nil {
		return fmt.Errorf("open WAPP %s: %w", args[0], err)
	}
	defer func() { _ = file.Close() }()

	reader, err := wapp.NewReader(file)
	if err != nil {
		return fmt.Errorf("read WAPP %s: %w", args[0], err)
	}
	info, err := findArtifactResource(reader.ListResources(), resourceID)
	if err != nil {
		return err
	}
	_, declared, err := artifact.ParseDeclaration(info.Meta)
	if err != nil {
		return fmt.Errorf("resource %s: %w", resourceID.String(), err)
	}
	if !declared {
		return fmt.Errorf("resource %s does not declare meta.artifact.format", resourceID.String())
	}
	filesystem, err := reader.GetFS(resourceID)
	if err != nil {
		return fmt.Errorf("open resource %s: %w", resourceID.String(), err)
	}
	registry, err := newArtifactRegistry()
	if err != nil {
		return err
	}

	packMetadata, err := reader.GetMetadata()
	if err != nil {
		return fmt.Errorf("read WAPP metadata: %w", err)
	}
	effect, err := artifact.NewPartialEffect(
		registry,
		nil,
		[]artifact.Resource{{
			Filesystem:    filesystem,
			Meta:          info.Meta,
			ModuleVersion: metadataString(packMetadata, "version"),
			ResourceID:    resourceID,
			Source:        args[0],
		}},
		root,
	)
	if err != nil {
		return err
	}
	if err := effect.Prepare(cmd.Context()); err != nil {
		return err
	}
	if err := effect.Commit(cmd.Context()); err != nil {
		return errors.Join(err, effect.Rollback(cmd.Context()))
	}
	results := effect.Results()
	if len(results) != 1 {
		_ = effect.Rollback(cmd.Context())
		return fmt.Errorf("materialized %d artifacts, expected 1", len(results))
	}
	if err := effect.Finalize(cmd.Context()); err != nil {
		return err
	}
	descriptor := results[0].Descriptor
	destination := results[0].Destination
	_, err = fmt.Fprintf(
		cmd.OutOrStdout(),
		"Materialized %s@%s to %s\n",
		descriptor.Identity,
		descriptor.Version,
		destination,
	)
	if err != nil {
		return fmt.Errorf("write materialization result: %w", err)
	}
	return nil
}

func parseArtifactResourceID(value string) (wapp.ID, error) {
	trimmed := strings.TrimSpace(value)
	namespace, name, found := strings.Cut(trimmed, ":")
	if !found ||
		namespace == "" ||
		name == "" ||
		namespace != strings.TrimSpace(namespace) ||
		name != strings.TrimSpace(name) ||
		strings.Contains(name, ":") {
		return wapp.ID{}, fmt.Errorf(
			"invalid artifact resource %q: expected full namespace:name", value)
	}
	return wapp.NewID(namespace, name), nil
}

func findArtifactResource(resources []wapp.ResourceInfo, id wapp.ID) (wapp.ResourceInfo, error) {
	for _, resource := range resources {
		if resource.ID.Equal(id) {
			return resource, nil
		}
	}
	return wapp.ResourceInfo{}, fmt.Errorf("artifact resource %s not found", id.String())
}

func metadataString(metadata wapp.Metadata, key string) string {
	value, _ := metadata[key].(string)
	return value
}
