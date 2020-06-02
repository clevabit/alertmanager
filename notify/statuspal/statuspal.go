package statuspal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/notify"
	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/alertmanager/types"
	commoncfg "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"net/http"
	"time"
)

// Notifier implements a Notifier for Statuspal notifications.
type Notifier struct {
	conf    *config.StatuspalConfig
	tmpl    *template.Template
	logger  log.Logger
	client  *http.Client
	retrier *notify.Retrier
}

// New returns a new Statuspal notifier.
func New(c *config.StatuspalConfig, t *template.Template, l log.Logger) (*Notifier, error) {
	client, err := commoncfg.NewClientFromConfig(*c.HTTPConfig, "statuspal", false)
	if err != nil {
		return nil, err
	}
	return &Notifier{
		conf:   c,
		tmpl:   t,
		logger: l,
		client: client,
		// Missing documentation therefore assuming only 5xx response codes are
		// recoverable.
		retrier: &notify.Retrier{},
	}, nil
}

const (
	statuspalActivityTypeTrigger = 1
	statuspalActivityTypeResolve = 4
)

// Notify implements the Notifier interface.
func (n *Notifier) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {
	var err error
	var (
		data = notify.GetTemplateData(ctx, n.tmpl, as, n.logger)
		tmpl = notify.TmplText(n.tmpl, data, &err)

		apiURL = n.conf.APIURL.Copy()
		apiKey = tmpl(string(n.conf.APIKey))
	)
	apiURL.Path += fmt.Sprintf("status_page/%s/incidents", n.conf.StatuspageDomain)

	buf, err := n.createStatuspalPayload(ctx, as...)
	if err != nil {
		return true, err
	}

	req, err := http.NewRequest("POST", apiURL.String(), buf)
	if err != nil {
		return true, notify.RedactURL(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", apiKey)

	resp, err := n.client.Do(req.WithContext(ctx))
	if err != nil {
		return true, notify.RedactURL(err)
	}
	defer notify.Drain(resp)

	return n.retrier.Check(resp.StatusCode, nil)
}

func (n *Notifier) createStatuspalPayload(ctx context.Context, as ...*types.Alert) (*bytes.Buffer, error) {
	key, err := notify.ExtractGroupKey(ctx)
	if err != nil {
		return nil, err
	}

	var (
		alerts = types.Alerts(as...)
		data   = notify.GetTemplateData(ctx, n.tmpl, as, n.logger)
		tmpl   = notify.TmplText(n.tmpl, data, &err)

		incidentType    = tmpl(n.conf.IncidentType)
		titleMessage    = tmpl(n.conf.TitleMessage)
		incidentMessage = tmpl(n.conf.IncidentMessage)
	)

	activityType := statuspalActivityTypeTrigger
	if alerts.Status() == model.AlertFiring {
		activityType = statuspalActivityTypeTrigger
	}

	if alerts.Status() == model.AlertResolved {
		activityType = statuspalActivityTypeResolve
	}

	titleMessage, truncated := notify.Truncate(titleMessage, 20480)
	if truncated {
		level.Debug(n.logger).Log("msg", "truncated titleMessage", "truncated_title_message", titleMessage, "incident", key)
	}

	incidentMessage, truncated = notify.Truncate(incidentMessage, 20480)
	if truncated {
		level.Debug(n.logger).Log("msg", "truncated incidentMessage", "truncated_incident_message", incidentMessage, "incident", key)
	}

	msg := map[string]interface{}{
		"title":       titleMessage,
		"service_ids": n.conf.ServiceIds,
		"type":        incidentType,
		"starts_at":   n.startsAt(as).UTC(),
		"incident_activities": []map[string]interface{}{
			{
				"activity_type_id": activityType,
				"description":      incidentMessage,
				"email_notify":     n.conf.NotifyEmail,
				"slack_notify":     n.conf.NotifySlack,
				"tweet":            n.conf.NotifyTweet,
			},
		},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(msg); err != nil {
		return nil, err
	}
	return &buf, nil
}

func (n *Notifier) startsAt(as []*types.Alert) time.Time {
	if len(as) > 0 {
		return as[0].StartsAt
	}
	return time.Now()
}
