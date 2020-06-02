package statuspal

import (
	"fmt"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/notify/test"
	commoncfg "github.com/prometheus/common/config"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestStatuspalRetry(t *testing.T) {
	notifier, err := New(
		&config.StatuspalConfig{
			APIKey:     config.Secret("secret"),
			HTTPConfig: &commoncfg.HTTPClientConfig{},
		},
		test.CreateTmpl(t),
		log.NewNopLogger(),
	)
	require.NoError(t, err)
	for statusCode, expected := range test.RetryTests(test.DefaultRetryCodes()) {
		actual, _ := notifier.retrier.Check(statusCode, nil)
		require.Equal(t, expected, actual, fmt.Sprintf("error on status %d", statusCode))
	}
}

func TestVictorOpsRedactedURL(t *testing.T) {
	ctx, u, fn := test.GetContextWithCancelingURL()
	defer fn()

	secret := "secret"
	notifier, err := New(
		&config.StatuspalConfig{
			APIURL:     &config.URL{URL: u},
			APIKey:     config.Secret(secret),
			HTTPConfig: &commoncfg.HTTPClientConfig{},
		},
		test.CreateTmpl(t),
		log.NewNopLogger(),
	)
	require.NoError(t, err)

	test.AssertNotifyLeaksNoSecret(t, ctx, notifier, secret)
}
