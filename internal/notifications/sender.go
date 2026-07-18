package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/bradleymackey/track-slash/internal/store"
)

type Sender interface {
	Send(context.Context, store.PushNotificationDelivery, store.PushNotificationPayload) (int, error)
}

type WebPushSender struct {
	publicKey  string
	privateKey string
	subscriber string
	client     webpush.HTTPClient
}

func NewWebPushSender(publicKey, privateKey, subscriber string, client webpush.HTTPClient) (*WebPushSender, error) {
	if publicKey == "" || privateKey == "" || subscriber == "" {
		return nil, errors.New("web push sender requires VAPID public key, private key, and subscriber")
	}
	return &WebPushSender{publicKey: publicKey, privateKey: privateKey, subscriber: subscriber, client: client}, nil
}

func (s *WebPushSender) Send(ctx context.Context, delivery store.PushNotificationDelivery, payload store.PushNotificationPayload) (int, error) {
	message, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	response, err := webpush.SendNotificationWithContext(ctx, message, &webpush.Subscription{
		Endpoint: delivery.Endpoint,
		Keys: webpush.Keys{
			P256dh: delivery.P256DH,
			Auth:   delivery.AuthSecret,
		},
	}, &webpush.Options{
		HTTPClient:      s.client,
		Subscriber:      s.subscriber,
		Topic:           payload.Tag,
		TTL:             3600,
		Urgency:         webpush.UrgencyNormal,
		VAPIDPublicKey:  s.publicKey,
		VAPIDPrivateKey: s.privateKey,
	})
	if response != nil {
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	}
	if err != nil {
		return 0, err
	}
	if response == nil {
		return 0, errors.New("web push provider returned no response")
	}
	return response.StatusCode, nil
}

var _ Sender = (*WebPushSender)(nil)
var _ webpush.HTTPClient = (*http.Client)(nil)
