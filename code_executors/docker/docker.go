package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"go.uber.org/zap"
)

type Executor struct {
	log *zap.Logger
	cli *client.Client
	cc  *container.Config
}

func NewExecutor(logger *zap.Logger) (*Executor, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, err
	}

	cc := &container.Config{
		Image:        "alpine",
		Tty:          false,
		OpenStdin:    true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	}

	return &Executor{
		log: logger,
		cc:  cc,
		cli: cli,
	}, nil
}

// Execute executes the given command in a docker container
// This is a blocking call
func (e *Executor) Execute(ctx context.Context, cmd string) error {
	e.log.Debug("executing command", zap.String("command", cmd))
	// execute command
	e.cc.Cmd = []string{"sh", "-c", cmd}

	// pre-pull image
	imageName := "docker.io/library/alpine:latest"
	err := e.pullImage(ctx, imageName)
	if err != nil {
		return err
	}

	e.log.Debug("finished checking the docker image")

	resp, err := e.cli.ContainerCreate(ctx, e.cc, nil, nil, nil, "")
	if err != nil {
		return err
	}

	waiter, err := e.cli.ContainerAttach(ctx, resp.ID, container.AttachOptions{
		Stdin:  true,
		Stdout: true,
		Stderr: true,
		Stream: true,
	})

	if err != nil {
		return err
	}
	defer waiter.Close()

	err = e.cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	if err != nil {
		return err
	}

	_, err = stdcopy.StdCopy(os.Stdout, os.Stderr, waiter.Reader)
	if err != nil {
		return err
	}

	status, errCh := e.cli.ContainerWait(ctx, resp.ID, container.WaitConditionNextExit)
	e.log.Debug("waiting for the container to exit")
	select {
	case err := <-errCh:
		e.log.Error("error", zap.Error(err))
	case st := <-status:
		if st.Error != nil {
			e.log.Debug("status", zap.String("msg", st.Error.Message))
		} else {
			e.log.Debug("status", zap.Int64("code", st.StatusCode))
		}
	}

	return nil
}

func (e *Executor) Close(context.Context) error {
	return e.cli.Close()
}

// private -----------------------

func (e *Executor) pullImage(ctx context.Context, imageName string) error {
	reader, err := e.cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = reader.Close()
	}()

	dec := json.NewDecoder(reader)
	for {
		var ps struct {
			Status   string `json:"status"`
			Progress string `json:"progress,omitempty"`
			Error    string `json:"error,omitempty"`
		}

		if err := dec.Decode(&ps); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return fmt.Errorf("failed to decode pull status: %w", err)
		}

		if ps.Error != "" {
			return fmt.Errorf("pull error: %s", ps.Error)
		}

		fmt.Printf("\r%s %s", ps.Status, ps.Progress)
	}

	return nil
}
