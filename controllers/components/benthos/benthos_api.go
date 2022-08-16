package benthos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
)

var (
	ErrAlreadyExists = errors.New("stream already exists")
	ErrNotFound      = errors.New("stream not found")
)

type BenthosStream struct {
	Active    bool           `json:"active"`
	Uptime    float64        `json:"uptime"`
	UptimeStr string         `json:"uptime_str"`
	Config    map[string]any `json:"config"`
}

type Api struct {
}

func (a *Api) CreateStream(ctx context.Context, address string, id string, config string) error {
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/streams/%s", address, id),
		bytes.NewBufferString(config))
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)

	data, _ := httputil.DumpRequest(req, true)
	fmt.Println(string(data))

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	switch rsp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusBadRequest: // Benthos responds with 400 if stream already exists
		return ErrAlreadyExists
	default:
		data, err := io.ReadAll(rsp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected status code %d: %s", rsp.StatusCode, string(data))
	}
}

func (a *Api) GetStream(ctx context.Context, address, id string) (*BenthosStream, error) {
	req, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/streams/%s", address, id),
		nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	switch rsp.StatusCode {
	case http.StatusOK:
		ret := &BenthosStream{}
		if err := json.NewDecoder(rsp.Body).Decode(ret); err != nil {
			return nil, err
		}
		return ret, nil
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		data, err := io.ReadAll(rsp.Body)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unexpected status code %d: %s", rsp.StatusCode, string(data))
	}
}

func (a *Api) UpdateStream(ctx context.Context, address string, id string, config string) error {
	req, err := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/streams/%s", address, id),
		bytes.NewBufferString(config))
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	switch rsp.StatusCode {
	case http.StatusOK:
		return nil
	default:
		data, err := io.ReadAll(rsp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected status code %d: %s", rsp.StatusCode, string(data))
	}
}
