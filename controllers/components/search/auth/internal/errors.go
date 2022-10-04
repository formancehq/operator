package internal

import (
	"fmt"

	"github.com/numary/auth/authclient"
)

func ConvertError(err error) error {
	if err == nil {
		return nil
	}
	if err, ok := err.(*authclient.GenericOpenAPIError); ok {
		return fmt.Errorf(string(err.Body()))
	}
	return err
}
