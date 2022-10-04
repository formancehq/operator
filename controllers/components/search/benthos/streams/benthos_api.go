package streams

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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

type ErrorResponse struct {
	LintErrors []string `json:"lint_errors,omitempty"`
}

type Api interface {
	CreateStream(ctx context.Context, address string, id string, config string) error
	GetStream(ctx context.Context, address, id string) (*BenthosStream, error)
	UpdateStream(ctx context.Context, address string, id string, config string) error
	DeleteStream(ctx context.Context, address string, id string) error
}

type DefaultApi struct {
}

func (a *DefaultApi) request(ctx context.Context, address, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method,
		fmt.Sprintf("%s%s", address, path),
		body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	return http.DefaultClient.Do(req)
}

func (a *DefaultApi) CreateStream(ctx context.Context, address string, id string, config string) error {

	rsp, err := a.request(ctx, address, http.MethodPost, fmt.Sprintf("/streams/%s", id), bytes.NewBufferString(config))
	if err != nil {
		return err
	}

	switch rsp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusBadRequest:
		data, err := io.ReadAll(rsp.Body)
		if err != nil {
			panic(err)
		}
		errorResponse := ErrorResponse{}
		if err := json.Unmarshal(data, &errorResponse); err == nil {
			if len(errorResponse.LintErrors) > 0 {
				return NewErrLintError(errorResponse.LintErrors)
			}
		}

		return fmt.Errorf("unexpected status code 400: %s", string(data))
	default:
		data, err := io.ReadAll(rsp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("unexpected status code %d: %s", rsp.StatusCode, string(data))
	}
}

type errLintError []string

func (errLintError) Is(e error) bool {
	_, ok := e.(errLintError)
	return ok
}

func (e errLintError) Error() string {
	return strings.Join(e, ", ")
}

func NewErrLintError(lintErrors []string) error {
	return errLintError(lintErrors)
}

func IsLintError(e error) bool {
	return errors.Is(e, errLintError{})
}

func (a *DefaultApi) GetStream(ctx context.Context, address, id string) (*BenthosStream, error) {
	rsp, err := a.request(ctx, address, http.MethodGet, fmt.Sprintf("/streams/%s", id), nil)
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

func (a *DefaultApi) UpdateStream(ctx context.Context, address string, id string, config string) error {
	rsp, err := a.request(ctx, address, http.MethodPut, fmt.Sprintf("/streams/%s", id), bytes.NewBufferString(config))
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

func (a *DefaultApi) DeleteStream(ctx context.Context, address string, id string) error {
	rsp, err := a.request(ctx, address, http.MethodDelete, fmt.Sprintf("/streams/%s", id), nil)
	if err != nil {
		return err
	}

	switch rsp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return ErrNotFound
	default:
		data, err := io.ReadAll(rsp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected status code %d: %s", rsp.StatusCode, string(data))
	}
}

func NewDefaultApi() *DefaultApi {
	return &DefaultApi{}
}

type inMemoryApi struct {
	configs map[string]string
}

func (i *inMemoryApi) CreateStream(ctx context.Context, address string, id string, config string) error {
	i.configs[id] = config
	return nil
}

func (i *inMemoryApi) GetStream(ctx context.Context, address, id string) (*BenthosStream, error) {
	_, ok := i.configs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &BenthosStream{
		Active: true,
	}, nil
}

func (i *inMemoryApi) UpdateStream(ctx context.Context, address string, id string, config string) error {
	_, ok := i.configs[id]
	if !ok {
		return ErrNotFound
	}
	i.configs[id] = config
	return nil
}

func (i *inMemoryApi) DeleteStream(ctx context.Context, address string, id string) error {
	_, ok := i.configs[id]
	if !ok {
		return ErrNotFound
	}
	delete(i.configs, id)
	return nil
}

func (i *inMemoryApi) reset() {
	i.configs = map[string]string{}
}

func newInMemoryApi() *inMemoryApi {
	return &inMemoryApi{
		configs: map[string]string{},
	}
}

var _ Api = &inMemoryApi{}
