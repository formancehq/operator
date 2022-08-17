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

type Api struct {
}

func (a *Api) request(ctx context.Context, address, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method,
		fmt.Sprintf("%s%s", address, path),
		body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	data, _ := httputil.DumpRequest(req, true)
	fmt.Println(string(data))

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	data, _ = httputil.DumpResponse(rsp, true)
	fmt.Println(string(data))

	return rsp, err
}

func (a *Api) CreateStream(ctx context.Context, address string, id string, config string) error {

	rsp, err := a.request(ctx, address, http.MethodPost, fmt.Sprintf("/streams/%s", id), bytes.NewBufferString(config))
	if err != nil {
		return err
	}

	switch rsp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusBadRequest: // Benthos responds with 400 if stream already exists
		errorResponse := ErrorResponse{}
		if err := json.NewDecoder(rsp.Body).Decode(&errorResponse); err != nil {
			return err
		}
		if len(errorResponse.LintErrors) > 0 {
			return NewErrLintError(errorResponse.LintErrors)
		}
		return ErrAlreadyExists
	default:
		data, err := io.ReadAll(rsp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf("unexpected status code %d: %s", rsp.StatusCode, string(data))
	}
}

type errLintError []string

func (e errLintError) Error() string {
	return strings.Join(e, ", ")
}

func NewErrLintError(lintErrors []string) error {
	return errLintError(lintErrors)
}

func (a *Api) GetStream(ctx context.Context, address, id string) (*BenthosStream, error) {
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

func (a *Api) UpdateStream(ctx context.Context, address string, id string, config string) error {
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
