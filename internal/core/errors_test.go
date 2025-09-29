package core

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewApplicationError(t *testing.T) {
	t.Parallel()

	err := NewApplicationError()

	require.NotNil(t, err)
	require.Empty(t, err.message)
	require.Empty(t, err.Error())
}

func TestApplicationErrorWithMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		message  string
		args     []any
		expected string
	}{
		{
			name:     "simple message",
			message:  "something went wrong",
			args:     nil,
			expected: "something went wrong",
		},
		{
			name:     "message with formatting",
			message:  "failed to process %s",
			args:     []any{"payment"},
			expected: "failed to process payment",
		},
		{
			name:     "message with multiple args",
			message:  "error in %s at line %d",
			args:     []any{"file.go", 42},
			expected: "error in file.go at line 42",
		},
		{
			name:     "message with complex formatting",
			message:  "stack %s not found in namespace %s",
			args:     []any{"production", "default"},
			expected: "stack production not found in namespace default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := NewApplicationError().WithMessage(tt.message, tt.args...)

			require.Equal(t, tt.expected, err.Error())
			require.Equal(t, tt.expected, err.message)
		})
	}
}

func TestApplicationErrorChaining(t *testing.T) {
	t.Parallel()

	// Test that WithMessage returns the same error instance (for chaining)
	err := NewApplicationError()
	result := err.WithMessage("test message")

	require.Equal(t, err, result, "WithMessage should return the same instance")
	require.Equal(t, "test message", err.Error())
}

func TestNewStackNotFoundError(t *testing.T) {
	t.Parallel()

	err := NewStackNotFoundError()

	require.NotNil(t, err)
	require.Equal(t, "stack not found", err.Error())
	require.IsType(t, &ApplicationError{}, err)
}

func TestNewPendingError(t *testing.T) {
	t.Parallel()

	err := NewPendingError()

	require.NotNil(t, err)
	require.Equal(t, "pending", err.Error())
	require.IsType(t, &ApplicationError{}, err)
}

func TestNewMissingSettingsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "custom message",
			message: "setting 'database.url' is required",
		},
		{
			name:    "another custom message",
			message: "missing configuration for module 'payments'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := NewMissingSettingsError(tt.message)

			require.NotNil(t, err)
			require.Equal(t, tt.message, err.Error())
			require.IsType(t, &ApplicationError{}, err)
		})
	}
}

func TestApplicationErrorIs(t *testing.T) {
	t.Parallel()

	err1 := NewApplicationError().WithMessage("error 1")
	err2 := NewApplicationError().WithMessage("error 2")
	err3 := &ApplicationError{}

	// Test that ApplicationError.Is works correctly
	require.True(t, err1.Is(err2), "ApplicationError should match another ApplicationError")
	require.True(t, err1.Is(err3), "ApplicationError should match empty ApplicationError")
	require.True(t, err2.Is(err1), "ApplicationError comparison should be symmetric")

	// Test with standard error
	standardErr := errors.New("standard error")
	require.False(t, err1.Is(standardErr), "ApplicationError should not match standard error")
}

func TestIsApplicationError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "ApplicationError instance",
			err:      NewApplicationError(),
			expected: true,
		},
		{
			name:     "StackNotFoundError",
			err:      NewStackNotFoundError(),
			expected: true,
		},
		{
			name:     "PendingError",
			err:      NewPendingError(),
			expected: true,
		},
		{
			name:     "MissingSettingsError",
			err:      NewMissingSettingsError("test"),
			expected: true,
		},
		{
			name:     "standard error",
			err:      errors.New("standard error"),
			expected: false,
		},
		{
			name:     "fmt error",
			err:      fmt.Errorf("formatted error"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := IsApplicationError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestApplicationErrorWrapping(t *testing.T) {
	t.Parallel()

	// Test that ApplicationError works with error wrapping
	appErr := NewApplicationError().WithMessage("app error")
	wrappedErr := fmt.Errorf("wrapped: %w", appErr)

	require.True(t, IsApplicationError(wrappedErr),
		"IsApplicationError should work with wrapped errors")

	require.True(t, errors.Is(wrappedErr, &ApplicationError{}),
		"errors.Is should work with wrapped ApplicationError")
}

func TestApplicationErrorTypes(t *testing.T) {
	t.Parallel()

	// Test that all constructor functions return ApplicationError
	errors := []error{
		NewApplicationError(),
		NewStackNotFoundError(),
		NewPendingError(),
		NewMissingSettingsError("test"),
	}

	for i, err := range errors {
		t.Run(fmt.Sprintf("error_%d", i), func(t *testing.T) {
			t.Parallel()

			_, ok := err.(*ApplicationError)
			require.True(t, ok, "Should be *ApplicationError type")

			require.True(t, IsApplicationError(err),
				"IsApplicationError should return true")
		})
	}
}

func TestApplicationErrorMessageModification(t *testing.T) {
	t.Parallel()

	err := NewApplicationError().WithMessage("initial message")
	require.Equal(t, "initial message", err.Error())

	// Modify the message
	err.WithMessage("updated message")
	require.Equal(t, "updated message", err.Error())

	// Modify again
	err.WithMessage("final message with %s", "formatting")
	require.Equal(t, "final message with formatting", err.Error())
}

func TestApplicationErrorInContext(t *testing.T) {
	t.Parallel()

	// Test how ApplicationError behaves in realistic reconciliation scenarios

	t.Run("stack not found during reconciliation", func(t *testing.T) {
		t.Parallel()

		err := NewStackNotFoundError()
		require.True(t, IsApplicationError(err))
		require.Equal(t, "stack not found", err.Error())
	})

	t.Run("pending resource not ready", func(t *testing.T) {
		t.Parallel()

		err := NewPendingError()
		require.True(t, IsApplicationError(err))
		require.Equal(t, "pending", err.Error())
	})

	t.Run("missing required settings", func(t *testing.T) {
		t.Parallel()

		settingKey := "postgres.ledger.uri"
		err := NewMissingSettingsError(fmt.Sprintf("required setting '%s' not found", settingKey))
		require.True(t, IsApplicationError(err))
		require.Contains(t, err.Error(), settingKey)
	})
}