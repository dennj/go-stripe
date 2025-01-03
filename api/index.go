package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/stripe/stripe-go/v81"
	portalsession "github.com/stripe/stripe-go/v81/billingportal/session"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/price"
	"github.com/stripe/stripe-go/v81/webhook"
	"golang.org/x/exp/slog"
)

// Handler is the exported function Vercel uses as the entry point
func Handler(w http.ResponseWriter, r *http.Request) {
	stripe.Key = "sk_test_51QZOy3Ln4tyvSVbGmXJ34MrjquL5Nks4yCDNQBcwonPD4z8jnfQAUrZE2mXRCjQUlcTjoiJA8EFqY8zUfLsAqztk009xXRYtII"

	switch r.URL.Path {
	case "/":
		htmlContent := `
	<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>Stripe Payment Test</title>
	</head>
	<body>
		<h1>Welcome to Stripe Payment Test</h1>
		<button id="payButton">Pay with Stripe</button>
	
		<script>
			document.getElementById('payButton').addEventListener('click', function() {
				fetch('/create-checkout-session', {
					method: 'POST',
					headers: {
						'Content-Type': 'application/x-www-form-urlencoded',
					},
					body: 'lookup_key=prod_RSME4fcFjlrqA4'
				}).then(function(response) {
					return response.json();
				}).then(function(session) {
					window.location.href = session.url; // Redirect to Stripe checkout page
				}).catch(function(error) {
					console.error('Error:', error);
				});
			});
		</script>
	</body>
	</html>
	`
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(htmlContent))

	case "/create-checkout-session":
		createCheckoutSession(w, r)
	case "/create-portal-session":
		createPortalSession(w, r)
	case "/webhook":
		handleWebhook(w, r)
	default:
		http.NotFound(w, r)
	}
}

func createCheckoutSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	r.ParseForm()
	lookup_key := r.PostFormValue("lookup_key")

	domain := "https://go-stripe.vercel.app"
	params := &stripe.PriceListParams{
		LookupKeys: stripe.StringSlice([]string{
			lookup_key,
		}),
	}
	i := price.List(params)
	if !i.Next() {
		log.Printf(">>>>>>>>>>>>>>>>>>>>>>>>>>> Add a price lookup key to checkout.html line 27 for the demo <<<<<<<<<<<<<<<<<<<<<<<<")
		http.Error(w, "Price not found", http.StatusBadRequest)
		return
	}

	price := i.Price()

	checkoutParams := &stripe.CheckoutSessionParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(price.ID),
				Quantity: stripe.Int64(1),
			},
		},
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			BillingCycleAnchor: stripe.Int64(1672531200),
		},
		SuccessURL:   stripe.String(domain + "/success.html?session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:    stripe.String(domain + "/cancel.html"),
		AutomaticTax: &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(true)},
	}

	s, err := session.New(checkoutParams)
	if err != nil {
		log.Printf("session.New: %v", err)
		http.Error(w, "Failed to create checkout session", http.StatusInternalServerError)
		return
	}

	// Correctly replace {CHECKOUT_SESSION_ID} with the actual ID
	updatedSuccessURL := strings.Replace(*checkoutParams.SuccessURL, "{CHECKOUT_SESSION_ID}", s.ID, -1)
	checkoutParams.SuccessURL = stripe.String(updatedSuccessURL)

	http.Redirect(w, r, s.URL, http.StatusSeeOther)
}

func createPortalSession(w http.ResponseWriter, r *http.Request) {
	domain := "https://go-stripe.vercel.app/"
	r.ParseForm()
	sessionId := r.PostFormValue("session_id")
	slog.Info("createPortalSession", "sessionId", sessionId)
	s, err := session.Get(sessionId, nil)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Printf("session.Get: %v", err)
		return
	}

	// Authenticate your user.
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(s.Customer.ID),
		ReturnURL: stripe.String(domain),
	}
	ps, err := portalsession.New(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Printf("ps.New: %v", err)
		return
	}
	log.Printf("ps.New: %v", ps.URL)

	http.Redirect(w, r, ps.URL, http.StatusSeeOther)
}

func handleWebhook(w http.ResponseWriter, req *http.Request) {
	const MaxBodyBytes = int64(65536)
	bodyReader := http.MaxBytesReader(w, req.Body, MaxBodyBytes)
	payload, err := io.ReadAll(bodyReader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading request body: %v\n", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	// Replace this endpoint secret with your endpoint's unique secret
	// If you are testing with the CLI, find the secret by running 'stripe listen'
	// If you are using an endpoint defined with the API or dashboard, look in your webhook settings
	// at https://dashboard.stripe.com/webhooks
	endpointSecret := "whsec_12345"
	signatureHeader := req.Header.Get("Stripe-Signature")
	event, err := webhook.ConstructEvent(payload, signatureHeader, endpointSecret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Webhook signature verification failed. %v\n", err)
		w.WriteHeader(http.StatusBadRequest) // Return a 400 error on a bad signature
		return
	}
	// Unmarshal the event data into an appropriate struct depending on its Type
	switch event.Type {
	case "customer.subscription.deleted":
		var subscription stripe.Subscription
		err := json.Unmarshal(event.Data.Raw, &subscription)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing webhook JSON: %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("Subscription deleted for %s.", subscription.ID)
		// Then define and call a func to handle the deleted subscription.
		// handleSubscriptionCanceled(subscription)
	case "customer.subscription.updated":
		var subscription stripe.Subscription
		err := json.Unmarshal(event.Data.Raw, &subscription)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing webhook JSON: %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("Subscription updated for %s.", subscription.ID)
		// Then define and call a func to handle the successful attachment of a PaymentMethod.
		// handleSubscriptionUpdated(subscription)
	case "customer.subscription.created":
		var subscription stripe.Subscription
		err := json.Unmarshal(event.Data.Raw, &subscription)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing webhook JSON: %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("Subscription created for %s.", subscription.ID)
		// Then define and call a func to handle the successful attachment of a PaymentMethod.
		// handleSubscriptionCreated(subscription)
	case "customer.subscription.trial_will_end":
		var subscription stripe.Subscription
		err := json.Unmarshal(event.Data.Raw, &subscription)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing webhook JSON: %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("Subscription trial will end for %s.", subscription.ID)
		// Then define and call a func to handle the successful attachment of a PaymentMethod.
		// handleSubscriptionTrialWillEnd(subscription)
	case "entitlements.active_entitlement_summary.updated":
		var subscription stripe.Subscription
		err := json.Unmarshal(event.Data.Raw, &subscription)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing webhook JSON: %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("Active entitlement summary updated for %s.", subscription.ID)
		// Then define and call a func to handle active entitlement summary updated.
		// handleEntitlementUpdated(subscription)
	default:
		fmt.Fprintf(os.Stderr, "Unhandled event type: %s\n", event.Type)
	}
	w.WriteHeader(http.StatusOK)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Printf("json.NewEncoder.Encode: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := io.Copy(w, &buf); err != nil {
		log.Printf("io.Copy: %v", err)
		return
	}
}
