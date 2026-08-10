// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"context"

	csapi "github.com/wippyai/runtime/api/cloudstorage"
	"github.com/wippyai/runtime/api/dispatcher"
)

type Dispatcher struct{}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{}
}

func (d *Dispatcher) Start(_ context.Context) error {
	return nil
}

func (d *Dispatcher) Stop(_ context.Context) error {
	return nil
}

func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(csapi.ListObjects, dispatcher.HandlerFunc(d.handleListObjects))
	register(csapi.DownloadObject, dispatcher.HandlerFunc(d.handleDownloadObject))
	register(csapi.UploadObject, dispatcher.HandlerFunc(d.handleUploadObject))
	register(csapi.DeleteObjects, dispatcher.HandlerFunc(d.handleDeleteObjects))
	register(csapi.PresignedGetURL, dispatcher.HandlerFunc(d.handlePresignedGetURL))
	register(csapi.PresignedPutURL, dispatcher.HandlerFunc(d.handlePresignedPutURL))
	register(csapi.HeadObject, dispatcher.HandlerFunc(d.handleHeadObject))
	register(csapi.CreateMultipartUpload, dispatcher.HandlerFunc(d.handleCreateMultipartUpload))
	register(csapi.PresignedUploadPartURLs, dispatcher.HandlerFunc(d.handlePresignedPartURLs))
	register(csapi.CompleteMultipartUpload, dispatcher.HandlerFunc(d.handleCompleteMultipartUpload))
	register(csapi.AbortMultipartUpload, dispatcher.HandlerFunc(d.handleAbortMultipartUpload))
	register(csapi.OpenReader, dispatcher.HandlerFunc(d.handleOpenReader))
}

// multipartStorage asserts the optional multipart capability on a provider.
func multipartStorage(s csapi.Storage) (csapi.MultipartStorage, error) {
	mp, ok := s.(csapi.MultipartStorage)
	if !ok {
		return nil, csapi.ErrMultipartUnsupported
	}
	return mp, nil
}

func (d *Dispatcher) handleListObjects(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*csapi.ListObjectsCmd)
	go func() {
		result, err := c.Storage.ListObjects(ctx, c.Options)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, csapi.ListObjectsResponse{Result: result, Error: err}, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleDownloadObject(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*csapi.DownloadObjectCmd)
	go func() {
		err := c.Storage.DownloadObject(ctx, c.Key, c.Writer, c.Options)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, csapi.DownloadObjectResponse{Error: err}, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleUploadObject(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*csapi.UploadObjectCmd)
	go func() {
		err := c.Storage.UploadObject(ctx, c.Key, c.Reader, c.Options)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, csapi.UploadObjectResponse{Error: err}, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleHeadObject(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*csapi.HeadObjectCmd)
	go func() {
		result, err := c.Storage.HeadObject(ctx, c.Key)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, csapi.HeadObjectResponse{Result: result, Error: err}, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleDeleteObjects(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*csapi.DeleteObjectsCmd)
	go func() {
		err := c.Storage.DeleteObjects(ctx, c.Keys)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, csapi.DeleteObjectsResponse{Error: err}, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handlePresignedGetURL(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*csapi.PresignedGetURLCmd)
	go func() {
		opts := &csapi.PresignedGetOptions{Expiration: c.Expiration}
		url, err := c.Storage.PresignedGetURL(ctx, c.Key, opts)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, csapi.PresignedGetURLResponse{URL: url, Error: err}, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handlePresignedPutURL(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*csapi.PresignedPutURLCmd)
	go func() {
		opts := &csapi.PresignedPutOptions{
			Expiration:    c.Expiration,
			ContentType:   c.ContentType,
			ContentLength: c.ContentLength,
		}
		url, err := c.Storage.PresignedPutURL(ctx, c.Key, opts)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, csapi.PresignedPutURLResponse{URL: url, Error: err}, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleCreateMultipartUpload(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*csapi.CreateMultipartUploadCmd)
	go func() {
		var result *csapi.CreateMultipartUploadResult
		mp, err := multipartStorage(c.Storage)
		if err == nil {
			result, err = mp.CreateMultipartUpload(ctx, c.Key, c.Options)
		}
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, csapi.CreateMultipartUploadResponse{Result: result, Error: err}, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handlePresignedPartURLs(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*csapi.PresignedPartURLsCmd)
	go func() {
		var urls []csapi.PresignedPartURL
		mp, err := multipartStorage(c.Storage)
		if err == nil {
			urls, err = mp.PresignedUploadPartURLs(ctx, c.Key, c.UploadID, c.Options)
		}
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, csapi.PresignedPartURLsResponse{URLs: urls, Error: err}, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleCompleteMultipartUpload(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*csapi.CompleteMultipartUploadCmd)
	go func() {
		var result *csapi.CompleteMultipartUploadResult
		mp, err := multipartStorage(c.Storage)
		if err == nil {
			result, err = mp.CompleteMultipartUpload(ctx, c.Key, c.UploadID, c.Parts)
		}
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, csapi.CompleteMultipartUploadResponse{Result: result, Error: err}, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleAbortMultipartUpload(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*csapi.AbortMultipartUploadCmd)
	go func() {
		mp, err := multipartStorage(c.Storage)
		if err == nil {
			err = mp.AbortMultipartUpload(ctx, c.Key, c.UploadID)
		}
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, csapi.AbortMultipartUploadResponse{Error: err}, nil)
		}
	}()
	return nil
}

func (d *Dispatcher) handleOpenReader(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c := cmd.(*csapi.OpenReaderCmd)
	go func() {
		var (
			size int64
			etag string
		)
		head, err := c.Storage.HeadObject(ctx, c.Key)
		if err == nil {
			size = head.Size
			etag = head.ETag
		}
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, csapi.OpenReaderResponse{Size: size, ETag: etag, Error: err}, nil)
		}
	}()
	return nil
}
