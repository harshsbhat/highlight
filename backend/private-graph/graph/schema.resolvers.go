package graph

// This file will be automatically regenerated based on the schema, any resolver implementations
// will be copied through when generating and any unknown code will be moved to the end.

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/highlight-run/highlight/backend/hlog"
	"github.com/highlight-run/highlight/backend/model"
	"github.com/highlight-run/highlight/backend/pricing"
	"github.com/highlight-run/highlight/backend/private-graph/graph/generated"
	modelInputs "github.com/highlight-run/highlight/backend/private-graph/graph/model"
	"github.com/highlight-run/highlight/backend/util"
	e "github.com/pkg/errors"
	"github.com/rs/xid"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	stripe "github.com/stripe/stripe-go"
	"golang.org/x/sync/errgroup"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

func (r *errorAlertResolver) ChannelsToNotify(ctx context.Context, obj *model.ErrorAlert) ([]*modelInputs.SanitizedSlackChannel, error) {
	return obj.GetChannelsToNotify()
}

func (r *errorAlertResolver) ExcludedEnvironments(ctx context.Context, obj *model.ErrorAlert) ([]*string, error) {
	return obj.GetExcludedEnvironments()
}

func (r *errorCommentResolver) Author(ctx context.Context, obj *model.ErrorComment) (*modelInputs.SanitizedAdmin, error) {
	admin := &model.Admin{}
	if err := r.DB.Where(&model.Admin{Model: model.Model{ID: obj.AdminId}}).First(&admin).Error; err != nil {
		return nil, e.Wrap(err, "Error finding admin for comment")
	}

	name := ""
	email := ""
	photo_url := ""

	if admin.Name != nil {
		name = *admin.Name
	}
	if admin.Email != nil {
		email = *admin.Email
	}
	if admin.PhotoURL != nil {
		photo_url = *admin.PhotoURL
	}

	sanitizedAdmin := &modelInputs.SanitizedAdmin{
		ID:       admin.ID,
		Name:     &name,
		Email:    email,
		PhotoURL: &photo_url,
	}

	return sanitizedAdmin, nil
}

func (r *errorGroupResolver) Event(ctx context.Context, obj *model.ErrorGroup) ([]*string, error) {
	return util.JsonStringToStringArray(obj.Event), nil
}

func (r *errorGroupResolver) StackTrace(ctx context.Context, obj *model.ErrorGroup) ([]*modelInputs.ErrorTrace, error) {
	if (obj.MappedStackTrace == nil || *obj.MappedStackTrace == "") && obj.StackTrace == "" {
		return nil, nil
	}
	var ret []*modelInputs.ErrorTrace
	stackTraceString := obj.StackTrace
	if obj.MappedStackTrace != nil && *obj.MappedStackTrace != "" {
		stackTraceString = *obj.MappedStackTrace
	}
	if err := json.Unmarshal([]byte(stackTraceString), &ret); err != nil {
		log.Error(e.Wrap(err, "error unmarshalling MappedStackTrace"))
		return nil, nil
	}
	return ret, nil
}

func (r *errorGroupResolver) MetadataLog(ctx context.Context, obj *model.ErrorGroup) ([]*modelInputs.ErrorMetadata, error) {
	ret := []*modelInputs.ErrorMetadata{}
	if err := json.Unmarshal([]byte(*obj.MetadataLog), &ret); err != nil {
		return nil, e.Wrap(err, "error unmarshaling error metadata")
	}
	filtered := []*modelInputs.ErrorMetadata{}
	for _, log := range ret {
		if log.ErrorID != 0 && log.SessionID != 0 && !log.Timestamp.IsZero() {
			filtered = append(filtered, log)
		}
	}
	return filtered, nil
}

func (r *errorGroupResolver) FieldGroup(ctx context.Context, obj *model.ErrorGroup) ([]*model.ErrorField, error) {
	if obj == nil || obj.FieldGroup == nil {
		return nil, nil
	}
	var fields []*model.ErrorField
	err := json.Unmarshal([]byte(*obj.FieldGroup), &fields)
	if err != nil {
		err := e.Wrap(err, "error converting field group to struct")
		return nil, err
	}
	var parsedFields []*model.ErrorField
	for _, f := range fields {
		if f.Name == "event" {
			continue
		}
		parsedFields = append(parsedFields, f)
	}
	return parsedFields, nil
}

func (r *errorGroupResolver) State(ctx context.Context, obj *model.ErrorGroup) (modelInputs.ErrorState, error) {
	switch obj.State {
	case model.ErrorGroupStates.OPEN:
		return modelInputs.ErrorStateOpen, nil
	case model.ErrorGroupStates.RESOLVED:
		return modelInputs.ErrorStateResolved, nil
	case model.ErrorGroupStates.IGNORED:
		return modelInputs.ErrorStateIgnored, nil
	default:
		return modelInputs.ErrorStateOpen, e.New("invalid error group state")
	}
}

func (r *errorObjectResolver) Event(ctx context.Context, obj *model.ErrorObject) ([]*string, error) {
	return util.JsonStringToStringArray(obj.Event), nil
}

func (r *errorObjectResolver) StackTrace(ctx context.Context, obj *model.ErrorObject) ([]interface{}, error) {
	frames := []interface{}{}
	if obj.StackTrace != nil {
		if err := json.Unmarshal([]byte(*obj.StackTrace), &frames); err != nil {
			return nil, fmt.Errorf("error decoding stack frame data: %v", err)
		}
	}
	return frames, nil
}

func (r *errorSegmentResolver) Params(ctx context.Context, obj *model.ErrorSegment) (*model.ErrorSearchParams, error) {
	params := &model.ErrorSearchParams{}
	if obj.Params == nil {
		return params, nil
	}
	if err := json.Unmarshal([]byte(*obj.Params), params); err != nil {
		return nil, e.Wrapf(err, "error unmarshaling segment params")
	}
	return params, nil
}

func (r *mutationResolver) CreateOrganization(ctx context.Context, name string) (*model.Organization, error) {
	admin, err := r.getCurrentAdmin(ctx)
	if err != nil {
		return nil, e.Wrap(err, "error getting admin")
	}

	c := &stripe.Customer{}
	if os.Getenv("REACT_APP_ONPREM") != "true" {
		params := &stripe.CustomerParams{}
		c, err = r.StripeClient.Customers.New(params)
		if err != nil {
			return nil, e.Wrap(err, "error creating stripe customer")
		}
	}

	org := &model.Organization{
		StripeCustomerID: &c.ID,
		Name:             &name,
		Admins:           []model.Admin{*admin},
		BillingEmail:     admin.Email,
	}
	if err := r.DB.Create(org).Error; err != nil {
		return nil, e.Wrap(err, "error creating org")
	}
	if err := r.DB.Create(&model.ErrorAlert{Alert: model.Alert{OrganizationID: org.ID, ExcludedEnvironments: nil, CountThreshold: 1, ChannelsToNotify: nil, Type: &model.AlertType.ERROR}}).Error; err != nil {
		return nil, e.Wrap(err, "error creating org")
	}
	if err := r.DB.Create(&model.SessionAlert{Alert: model.Alert{OrganizationID: org.ID, ExcludedEnvironments: nil, CountThreshold: 1, ChannelsToNotify: nil, Type: &model.AlertType.NEW_USER}}).Error; err != nil {
		return nil, e.Wrap(err, "error creating session alert for new org")
	}
	if err := r.DB.Create(&model.SessionAlert{Alert: model.Alert{OrganizationID: org.ID, ExcludedEnvironments: nil, CountThreshold: 1, ChannelsToNotify: nil, Type: &model.AlertType.TRACK_PROPERTIES}}).Error; err != nil {
		return nil, e.Wrap(err, "error creating session alert for new org")
	}
	if err := r.DB.Create(&model.SessionAlert{Alert: model.Alert{OrganizationID: org.ID, ExcludedEnvironments: nil, CountThreshold: 1, ChannelsToNotify: nil, Type: &model.AlertType.USER_PROPERTIES}}).Error; err != nil {
		return nil, e.Wrap(err, "error creating session alert for new org")
	}
	return org, nil
}

func (r *mutationResolver) EditOrganization(ctx context.Context, id int, name *string, billingEmail *string) (*model.Organization, error) {
	org, err := r.isAdminInOrganization(ctx, id)
	if err != nil {
		return nil, e.Wrap(err, "error querying org")
	}
	if err := r.DB.Model(org).Updates(&model.Organization{
		Name:         name,
		BillingEmail: billingEmail,
	}).Error; err != nil {
		return nil, e.Wrap(err, "error updating org fields")
	}
	return org, nil
}

func (r *mutationResolver) MarkSessionAsViewed(ctx context.Context, id int, viewed *bool) (*model.Session, error) {
	_, err := r.canAdminModifySession(ctx, id)
	if err != nil {
		return nil, e.Wrap(err, "admin not session owner")
	}
	session := &model.Session{}
	if err := r.DB.Where(&model.Session{Model: model.Model{ID: id}}).First(&session).Updates(&model.Session{
		Viewed: viewed,
	}).Error; err != nil {
		return nil, e.Wrap(err, "error writing session as viewed")
	}

	return session, nil
}

func (r *mutationResolver) MarkSessionAsStarred(ctx context.Context, id int, starred *bool) (*model.Session, error) {
	_, err := r.canAdminModifySession(ctx, id)
	if err != nil {
		return nil, e.Wrap(err, "admin not session owner")
	}
	session := &model.Session{}
	if err := r.DB.Where(&model.Session{Model: model.Model{ID: id}}).First(&session).Updates(&model.Session{
		Starred: starred,
	}).Error; err != nil {
		return nil, e.Wrap(err, "error writing session as starred")
	}

	return session, nil
}

func (r *mutationResolver) UpdateErrorGroupState(ctx context.Context, id int, state string) (*model.ErrorGroup, error) {
	_, err := r.isAdminErrorGroupOwner(ctx, id)
	if err != nil {
		return nil, e.Wrap(err, "admin not errorGroup owner")
	}

	errorGroup := &model.ErrorGroup{}
	if err := r.DB.Where(&model.ErrorGroup{Model: model.Model{ID: id}}).First(&errorGroup).Updates(&model.ErrorGroup{
		State: state,
	}).Error; err != nil {
		return nil, e.Wrap(err, "error writing errorGroup state")
	}

	return errorGroup, nil
}

func (r *mutationResolver) DeleteOrganization(ctx context.Context, id int) (*bool, error) {
	_, err := r.isAdminInOrganization(ctx, id)
	if err != nil {
		return nil, e.Wrap(err, "admin is not in organization")
	}
	if err := r.DB.Delete(&model.Organization{Model: model.Model{ID: id}}).Error; err != nil {
		return nil, e.Wrap(err, "error deleting organization")
	}
	return &model.T, nil
}

func (r *mutationResolver) SendAdminInvite(ctx context.Context, organizationID int, email string, baseURL string) (*string, error) {
	org, err := r.isAdminInOrganization(ctx, organizationID)
	if err != nil {
		return nil, e.Wrap(err, "error querying org")
	}
	admin, err := r.getCurrentAdmin(ctx)
	if err != nil {
		return nil, e.Wrap(err, "error querying admin")
	}
	var secret string
	if org.Secret == nil {
		uid := xid.New().String()
		if err := r.DB.Model(org).Updates(&model.Organization{Secret: &uid}).Error; err != nil {
			return nil, e.Wrap(err, "error updating uid in org secret")
		}
		secret = uid
	} else {
		secret = *org.Secret
	}
	inviteLink := baseURL + "/" + strconv.Itoa(organizationID) + "/invite/" + secret
	to := &mail.Email{Address: email}

	m := mail.NewV3Mail()
	from := mail.NewEmail("Highlight", "notifications@highlight.run")
	m.SetFrom(from)
	m.SetTemplateID(SendAdminInviteEmailTemplateID)

	p := mail.NewPersonalization()
	p.AddTos(to)
	p.SetDynamicTemplateData("Admin_Invitor", admin.Name)
	p.SetDynamicTemplateData("Organization_Name", org.Name)
	p.SetDynamicTemplateData("Invite_Link", inviteLink)

	m.AddPersonalizations(p)
	if resp, sendGridErr := r.MailClient.Send(m); sendGridErr != nil || resp.StatusCode >= 300 {
		estr := "error sending sendgrid email -> "
		estr += fmt.Sprintf("resp-code: %v; ", resp)
		if sendGridErr != nil {
			estr += fmt.Sprintf("err: %v", sendGridErr.Error())
		}
		return nil, e.New(estr)
	}
	return &email, nil
}

func (r *mutationResolver) AddAdminToOrganization(ctx context.Context, organizationID int, inviteID string) (*int, error) {
	org := &model.Organization{}
	if err := r.DB.Where(&model.Organization{Model: model.Model{ID: organizationID}}).First(&org).Error; err != nil {
		return nil, e.Wrap(err, "error querying org")
	}
	if org.Secret == nil || (org.Secret != nil && *org.Secret != inviteID) {
		return nil, e.New("invalid invite id")
	}
	admin, err := r.getCurrentAdmin(ctx)
	if err != nil {
		return nil, e.New("error querying admin")
	}
	if err := r.DB.Model(org).Association("Admins").Append(admin); err != nil {
		return nil, e.Wrap(err, "error adding admin to association")
	}
	return &org.ID, nil
}

func (r *mutationResolver) DeleteAdminFromOrganization(ctx context.Context, organizationID int, adminID int) (*int, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin is not in organization")
	}
	admin, err := r.getCurrentAdmin(ctx)
	if err != nil {
		return nil, e.New("error querying admin while deleting admin from organization")
	}
	if admin.ID == adminID {
		return nil, e.New("Admin tried deleting themselves from the organization")
	}

	if err := r.DB.Model(&model.Organization{Model: model.Model{ID: organizationID}}).Association("Admins").Delete(model.Admin{Model: model.Model{ID: adminID}}); err != nil {
		return nil, e.Wrap(err, "error deleting admin from organization")
	}

	return &adminID, nil
}

func (r *mutationResolver) AddSlackIntegrationToWorkspace(ctx context.Context, organizationID int, code string, redirectPath string) (*bool, error) {
	var (
		SLACK_CLIENT_ID     string
		SLACK_CLIENT_SECRET string
	)
	org, err := r.isAdminInOrganization(ctx, organizationID)
	if err != nil {
		return nil, e.Wrap(err, "admin is not in organization")
	}
	redirect := os.Getenv("FRONTEND_URI")
	redirect += "/" + strconv.Itoa(organizationID) + "/" + redirectPath
	if tempSlackClientID, ok := os.LookupEnv("SLACK_CLIENT_ID"); ok && tempSlackClientID != "" {
		SLACK_CLIENT_ID = tempSlackClientID
	}
	if tempSlackClientSecret, ok := os.LookupEnv("SLACK_CLIENT_SECRET"); ok && tempSlackClientSecret != "" {
		SLACK_CLIENT_SECRET = tempSlackClientSecret
	}
	resp, err := slack.
		GetOAuthV2Response(
			&http.Client{},
			SLACK_CLIENT_ID,
			SLACK_CLIENT_SECRET,
			code,
			redirect,
		)
	if err != nil {
		return nil, e.Wrap(err, "error getting slack oauth response")
	}
	existingChannels, err := org.IntegratedSlackChannels()
	if err != nil {
		return nil, e.Wrap(err, "error retrieving existing slack channels")
	}
	for _, ch := range existingChannels {
		if ch.WebhookChannelID == resp.IncomingWebhook.ChannelID {
			return nil, e.New("this channel has already been connected to your workspace")
		}
	}
	existingChannels = append(existingChannels, model.SlackChannel{
		WebhookAccessToken: resp.AccessToken,
		WebhookURL:         resp.IncomingWebhook.URL,
		WebhookChannelID:   resp.IncomingWebhook.ChannelID,
		WebhookChannel:     resp.IncomingWebhook.Channel,
	})
	channelBytes, err := json.Marshal(existingChannels)
	if err != nil {
		return nil, e.Wrap(err, "error marshaling existing channels")
	}
	channelString := string(channelBytes)
	if err := r.DB.Model(org).Updates(&model.Organization{
		SlackChannels: &channelString,
	}).Error; err != nil {
		return nil, e.Wrap(err, "error updating org fields")
	}

	baseMessage := "👋 Hello from Highlight!"
	if name := org.Name; name != nil {
		baseMessage += fmt.Sprintf(" We'll send messages here based on your alert preferences for %v, which can be configured at https://app.highlight.run/%v/alerts.", *name, org.ID)
	}
	msg := slack.WebhookMessage{Text: baseMessage}
	if err := slack.PostWebhook(resp.IncomingWebhook.URL, &msg); err != nil {
		log.Error(e.Wrap(err, "failed to send hello alert slack message"))
	}
	return &model.T, nil
}

func (r *mutationResolver) CreateSegment(ctx context.Context, organizationID int, name string, params modelInputs.SearchParamsInput) (*model.Segment, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin is not in organization")
	}
	modelParams := InputToParams(&params)
	// Convert to json to store in the db.
	paramBytes, err := json.Marshal(modelParams)
	if err != nil {
		return nil, e.Wrap(err, "error unmarshaling search params")
	}
	paramString := string(paramBytes)

	segment := &model.Segment{
		Name:           &name,
		Params:         &paramString,
		OrganizationID: organizationID,
	}
	if err := r.DB.Create(segment).Error; err != nil {
		return nil, e.Wrap(err, "error creating segment")
	}
	return segment, nil
}

func (r *mutationResolver) EmailSignup(ctx context.Context, email string) (string, error) {
	apiKey := os.Getenv("APOLLO_IO_API_KEY")

	type MatchRequest struct {
		ApiKey string `json:"api_key"`
		Email  string `json:"email"`
	}
	type MatchResponse struct {
		Person map[string]interface{} `json:"person"`
	}

	matchRequest := &MatchRequest{ApiKey: apiKey, Email: email}
	matchResponse := &MatchResponse{}
	err := util.RestRequest("https://api.apollo.io/v1/people/match", "POST", matchRequest, matchResponse)
	if err != nil {
		log.Errorf("error sending match request: %v", err)
	}

	contactString := ""
	contactBytes, err := json.MarshalIndent(matchResponse.Person, "", "  ")
	if err == nil {
		contactString = string(contactBytes)
	} else {
		log.Errorf("error marshaling: %v", err)
	}

	contactStringShort := ""
	shortContactMap := make(map[string]string)
	for key, val := range matchResponse.Person {
		if valString, ok := val.(string); ok {
			shortContactMap[key] = valString
		}
	}
	contactBytesShort, err := json.MarshalIndent(shortContactMap, "", "  ")
	if err == nil {
		contactStringShort = string(contactBytesShort)
	} else {
		log.Errorf("error marshaling short: %v", err)
	}
	model.DB.Create(&model.EmailSignup{
		Email:               email,
		ApolloData:          contactString,
		ApolloDataShortened: contactStringShort,
	})

	type ContactsRequest struct {
		ApiKey string `json:"api_key"`
		Email  string `json:"email"`
	}
	type Contact struct {
		ID string `json:"id"`
	}
	type ContactsResponse struct {
		Contact Contact `json:"contact"`
	}
	contactsRequest := &ContactsRequest{ApiKey: apiKey, Email: email}
	contactsResponse := &ContactsResponse{}
	err = util.RestRequest("https://api.apollo.io/v1/contacts", "POST", contactsRequest, contactsResponse)
	if err != nil {
		log.Errorf("error sending contacts request: %v", err)
		return email, nil
	}

	type SequenceRequest struct {
		ApiKey                      string   `json:"api_key"`
		ContactIDs                  []string `json:"contact_ids"`
		EmailerCampaignID           string   `json:"emailer_campaign_id"`
		SendEmailFromEmailAccountID string   `json:"send_email_from_email_account_id"`
	}
	type SequenceResponse struct {
		Contacts json.RawMessage `json:"contacts"`
	}
	sequenceRequest := &SequenceRequest{
		ApiKey:                      apiKey,
		ContactIDs:                  []string{contactsResponse.Contact.ID},
		EmailerCampaignID:           "60fb134ce97fa1014c1cc141", // Represents the sequence ID for "Landing Page Signups"
		SendEmailFromEmailAccountID: "6053cd5ef93cca00e498990f", // Respresents the ID for Jay's email account (jay@highlight.run)
	}
	sequenceResponse := &SequenceResponse{}
	url := fmt.Sprintf("https://api.apollo.io/v1/emailer_campaigns/%v/add_contact_ids", sequenceRequest.EmailerCampaignID)
	err = util.RestRequest(url, "POST", sequenceRequest, sequenceResponse)
	if err != nil {
		log.Errorf("error sending contacts request: %v", err)
		return email, nil
	}

	return email, nil
}

func (r *mutationResolver) EditSegment(ctx context.Context, id int, organizationID int, params modelInputs.SearchParamsInput) (*bool, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin is not in organization")
	}
	modelParams := InputToParams(&params)
	// Convert to json to store in the db.
	paramBytes, err := json.Marshal(modelParams)
	if err != nil {
		return nil, e.Wrap(err, "error unmarshaling search params")
	}
	paramString := string(paramBytes)
	if err := r.DB.Model(&model.Segment{Model: model.Model{ID: id}}).Updates(&model.Segment{
		Params: &paramString,
	}).Error; err != nil {
		return nil, e.Wrap(err, "error writing new recording settings")
	}
	return &model.T, nil
}

func (r *mutationResolver) DeleteSegment(ctx context.Context, segmentID int) (*bool, error) {
	_, err := r.isAdminSegmentOwner(ctx, segmentID)
	if err != nil {
		return nil, e.Wrap(err, "admin is not segment owner")
	}
	if err := r.DB.Delete(&model.Segment{Model: model.Model{ID: segmentID}}).Error; err != nil {
		return nil, e.Wrap(err, "error deleting segment")
	}
	return &model.T, nil
}

func (r *mutationResolver) CreateErrorSegment(ctx context.Context, organizationID int, name string, params modelInputs.ErrorSearchParamsInput) (*model.ErrorSegment, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin is not in organization")
	}
	modelParams := ErrorInputToParams(&params)
	// Convert to json to store in the db.
	paramBytes, err := json.Marshal(modelParams)
	if err != nil {
		return nil, e.Wrap(err, "error unmarshaling search params")
	}
	paramString := string(paramBytes)

	segment := &model.ErrorSegment{
		Name:           &name,
		Params:         &paramString,
		OrganizationID: organizationID,
	}
	if err := r.DB.Create(segment).Error; err != nil {
		return nil, e.Wrap(err, "error creating segment")
	}
	return segment, nil
}

func (r *mutationResolver) EditErrorSegment(ctx context.Context, id int, organizationID int, params modelInputs.ErrorSearchParamsInput) (*bool, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin is not in organization")
	}
	modelParams := ErrorInputToParams(&params)
	// Convert to json to store in the db.
	paramBytes, err := json.Marshal(modelParams)
	if err != nil {
		return nil, e.Wrap(err, "error unmarshaling search params")
	}
	paramString := string(paramBytes)
	if err := r.DB.Model(&model.ErrorSegment{Model: model.Model{ID: id}}).Updates(&model.ErrorSegment{
		Params: &paramString,
	}).Error; err != nil {
		return nil, e.Wrap(err, "error writing new recording settings")
	}
	return &model.T, nil
}

func (r *mutationResolver) DeleteErrorSegment(ctx context.Context, segmentID int) (*bool, error) {
	_, err := r.isAdminErrorSegmentOwner(ctx, segmentID)
	if err != nil {
		return nil, e.Wrap(err, "admin is not error segment owner")
	}
	if err := r.DB.Delete(&model.ErrorSegment{Model: model.Model{ID: segmentID}}).Error; err != nil {
		return nil, e.Wrap(err, "error deleting segment")
	}
	return &model.T, nil
}

func (r *mutationResolver) EditRecordingSettings(ctx context.Context, organizationID int, details *string) (*model.RecordingSettings, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}
	rec := &model.RecordingSettings{}
	res := r.DB.Where(&model.RecordingSettings{Model: model.Model{ID: organizationID}}).First(&rec)
	if err := res.Error; err != nil {
		return nil, e.Wrap(err, "error querying record")
	}
	if err := r.DB.Model(rec).Updates(&model.RecordingSettings{
		OrganizationID: organizationID,
		Details:        details,
	}).Error; err != nil {
		return nil, e.Wrap(err, "error writing new recording settings")
	}
	return rec, nil
}

func (r *mutationResolver) CreateOrUpdateSubscription(ctx context.Context, organizationID int, planType modelInputs.PlanType) (*string, error) {
	org, err := r.isAdminInOrganization(ctx, organizationID)
	if err != nil {
		return nil, e.Wrap(err, "admin is not in organization")
	}

	// For older workspaces, if there's no customer ID, we create a StripeCustomer obj.
	if org.StripeCustomerID == nil {
		params := &stripe.CustomerParams{}
		c, err := r.StripeClient.Customers.New(params)
		if err != nil {
			return nil, e.Wrap(err, "error creating stripe customer")
		}
		if err := r.DB.Model(org).Updates(&model.Organization{
			StripeCustomerID: &c.ID,
		}).Error; err != nil {
			return nil, e.Wrap(err, "error updating org fields")
		}
		org.StripeCustomerID = &c.ID
	}

	// Check if there's already a subscription on the user. If there is, we do an update and return early.
	params := &stripe.CustomerParams{}
	params.AddExpand("subscriptions")
	c, err := r.StripeClient.Customers.Get(*org.StripeCustomerID, params)
	if err != nil {
		return nil, e.Wrap(err, "couldn't retrieve stripe customer data")
	}
	// If there's a single subscription on the user and a single price item on the subscription
	if len(c.Subscriptions.Data) == 1 && len(c.Subscriptions.Data[0].Items.Data) == 1 {
		plan := pricing.ToPriceID(planType)
		subscriptionParams := &stripe.SubscriptionParams{
			CancelAtPeriodEnd: stripe.Bool(false),
			ProrationBehavior: stripe.String(string(stripe.SubscriptionProrationBehaviorCreateProrations)),
			Items: []*stripe.SubscriptionItemsParams{
				{
					ID:   stripe.String(c.Subscriptions.Data[0].Items.Data[0].ID),
					Plan: &plan,
				},
			},
		}
		_, err := r.StripeClient.Subscriptions.Update(c.Subscriptions.Data[0].ID, subscriptionParams)
		if err != nil {
			return nil, e.Wrap(err, "couldn't update subscription")
		}
		organization := model.Organization{Model: model.Model{ID: organizationID}}
		if err := r.DB.Model(&organization).Updates(model.Organization{StripePriceID: &plan}).Error; err != nil {
			return nil, e.Wrap(err, "error setting stripe_plan_id on organization")
		}

		// mark sessions as within billing quota on plan upgrade
		// this is done when the user is already signed up for some sort of billing plan
		if c.Subscriptions.Data[0].Items.Data[0].Plan != nil {
			go r.UpdateSessionsVisibility(organizationID, planType, pricing.FromPriceID(c.Subscriptions.Data[0].Items.Data[0].Plan.ID))
		} else {
			log.Error("error getting original plan data from stripe client")
		}

		ret := ""
		return &ret, nil
	}

	// If there's no existing subscription, we create a checkout.
	plan := pricing.ToPriceID(planType)
	checkoutSessionParams := &stripe.CheckoutSessionParams{
		SuccessURL: stripe.String(os.Getenv("FRONTEND_URI") + "/" + strconv.Itoa(organizationID) + "/billing/success"),
		CancelURL:  stripe.String(os.Getenv("FRONTEND_URI") + "/" + strconv.Itoa(organizationID) + "/billing/checkoutCanceled"),
		PaymentMethodTypes: stripe.StringSlice([]string{
			"card",
		}),
		Customer: org.StripeCustomerID,
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Items: []*stripe.CheckoutSessionSubscriptionDataItemsParams{
				{
					Plan: stripe.String(plan),
				},
			},
		},
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
	}
	checkoutSessionParams.AddExtra("allow_promotion_codes", "true")

	stripeSession, err := r.StripeClient.CheckoutSessions.New(checkoutSessionParams)
	if err != nil {
		return nil, e.Wrap(err, "error creating CheckoutSession in stripe")
	}

	organization := model.Organization{Model: model.Model{ID: organizationID}}
	if err := r.DB.Model(&organization).Updates(model.Organization{StripePriceID: &plan}).Error; err != nil {
		return nil, e.Wrap(err, "error setting stripe_plan_id on organization")
	}
	// mark sessions as within billing quota on plan upgrade
	// this code is repeated as the first time, the user already has a billing plan and the function returns early.
	// here, the user doesn't already have a billing plan, so it's considered an upgrade unless the plan is free
	go r.UpdateSessionsVisibility(organizationID, planType, modelInputs.PlanTypeFree)

	return &stripeSession.ID, nil
}

func (r *mutationResolver) CreateSessionComment(ctx context.Context, organizationID int, sessionID int, sessionTimestamp int, text string, textForEmail string, xCoordinate float64, yCoordinate float64, taggedAdmins []*modelInputs.SanitizedAdminInput, sessionURL string, time float64, authorName string, sessionImage *string) (*model.SessionComment, error) {
	admin, err := r.getCurrentAdmin(ctx)
	if admin == nil || err != nil {
		return nil, e.Wrap(err, "Unable to retrieve admin info")
	}

	// All viewers can leave a comment, including guests
	if _, err := r.canAdminViewSession(ctx, sessionID); err != nil {
		return nil, e.Wrap(err, "admin cannot leave a comment")
	}

	admins := []model.Admin{}
	for _, a := range taggedAdmins {
		admins = append(admins,
			model.Admin{
				Model: model.Model{ID: a.ID},
			},
		)
	}

	sessionComment := &model.SessionComment{
		Admins:         admins,
		OrganizationID: organizationID,
		AdminId:        admin.Model.ID,
		SessionId:      sessionID,
		Timestamp:      sessionTimestamp,
		Text:           text,
		XCoordinate:    xCoordinate,
		YCoordinate:    yCoordinate,
	}
	createSessionCommentSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.createSessionComment",
		tracer.ResourceName("db.createSessionComment"), tracer.Tag("org_id", organizationID))
	if err := r.DB.Create(sessionComment).Error; err != nil {
		return nil, e.Wrap(err, "error creating session comment")
	}
	createSessionCommentSpan.Finish()

	commentMentionEmailSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.createSessionComment",
		tracer.ResourceName("sendgrid.sendCommentMention"), tracer.Tag("org_id", organizationID), tracer.Tag("count", len(taggedAdmins)))
	if len(taggedAdmins) > 0 {
		tos := []*mail.Email{}

		for _, admin := range taggedAdmins {
			tos = append(tos, &mail.Email{Address: admin.Email})
		}
		m := mail.NewV3Mail()
		from := mail.NewEmail("Highlight", "notifications@highlight.run")
		viewLink := fmt.Sprintf("%v?commentId=%v&ts=%v", sessionURL, sessionComment.ID, time)
		m.SetFrom(from)
		m.SetTemplateID(SendGridSessionCommentEmailTemplateID)

		p := mail.NewPersonalization()
		p.AddTos(tos...)
		p.SetDynamicTemplateData("Author_Name", authorName)
		p.SetDynamicTemplateData("Comment_Link", viewLink)
		p.SetDynamicTemplateData("Comment_Body", textForEmail)
		p.SetDynamicTemplateData("Session_Image", sessionImage)

		if sessionImage != nil {
			a := mail.NewAttachment()
			a.SetContent(*sessionImage)
			a.SetFilename("session-image.png")
			a.SetContentID("sessionImage")
			a.SetType("image/png")
			m.AddAttachment(a)
		}

		m.AddPersonalizations(p)

		_, err := r.MailClient.Send(m)
		if err != nil {
			return nil, fmt.Errorf("error sending sendgrid email for comments mentions: %v", err)
		}
	}
	commentMentionEmailSpan.Finish()
	return sessionComment, nil
}

func (r *mutationResolver) DeleteSessionComment(ctx context.Context, id int) (*bool, error) {
	var sessionComment model.SessionComment
	if err := r.DB.Where(model.SessionComment{Model: model.Model{ID: id}}).First(&sessionComment).Error; err != nil {
		return nil, e.Wrap(err, "error querying session comment")
	}
	_, err := r.canAdminModifySession(ctx, sessionComment.SessionId)
	if err != nil {
		return nil, e.Wrap(err, "admin is not session owner")
	}
	if err := r.DB.Delete(&model.SessionComment{Model: model.Model{ID: id}}).Error; err != nil {
		return nil, e.Wrap(err, "error session comment")
	}
	return &model.T, nil
}

func (r *mutationResolver) CreateErrorComment(ctx context.Context, organizationID int, errorGroupID int, text string, textForEmail string, taggedAdmins []*modelInputs.SanitizedAdminInput, errorURL string, authorName string) (*model.ErrorComment, error) {
	admin, err := r.getCurrentAdmin(ctx)
	if admin == nil || err != nil {
		return nil, e.Wrap(err, "Unable to retrieve admin info")
	}

	if _, err := r.isAdminErrorGroupOwner(ctx, errorGroupID); err != nil {
		return nil, e.Wrap(err, "admin is not in organization")
	}

	admins := []model.Admin{}
	for _, a := range taggedAdmins {
		admins = append(admins,
			model.Admin{
				Model: model.Model{ID: a.ID},
			},
		)
	}

	errorComment := &model.ErrorComment{
		Admins:         admins,
		OrganizationID: organizationID,
		AdminId:        admin.Model.ID,
		ErrorId:        errorGroupID,
		Text:           text,
	}
	createErrorCommentSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.createErrorComment",
		tracer.ResourceName("db.createErrorComment"), tracer.Tag("org_id", organizationID))
	if err := r.DB.Create(errorComment).Error; err != nil {
		return nil, e.Wrap(err, "error creating error comment")
	}
	createErrorCommentSpan.Finish()

	commentMentionEmailSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.createErrorComment",
		tracer.ResourceName("sendgrid.sendCommentMention"), tracer.Tag("org_id", organizationID), tracer.Tag("count", len(taggedAdmins)))
	if len(taggedAdmins) > 0 {
		tos := []*mail.Email{}

		for _, admin := range taggedAdmins {
			tos = append(tos, &mail.Email{Address: admin.Email})
		}
		m := mail.NewV3Mail()
		from := mail.NewEmail("Highlight", "notifications@highlight.run")
		viewLink := fmt.Sprintf("%v", errorURL)
		m.SetFrom(from)
		m.SetTemplateID(SendGridErrorCommentEmailTemplateId)

		p := mail.NewPersonalization()
		p.AddTos(tos...)
		p.SetDynamicTemplateData("Author_Name", authorName)
		p.SetDynamicTemplateData("Comment_Link", viewLink)
		p.SetDynamicTemplateData("Comment_Body", textForEmail)

		m.AddPersonalizations(p)

		_, err := r.MailClient.Send(m)
		if err != nil {
			return nil, fmt.Errorf("error sending sendgrid email for comments mentions: %v", err)
		}
	}
	commentMentionEmailSpan.Finish()
	return errorComment, nil
}

func (r *mutationResolver) DeleteErrorComment(ctx context.Context, id int) (*bool, error) {
	var errorGroupID int
	if err := r.DB.Table("error_comments").Select("error_id").Where("id=?", id).Scan(&errorGroupID).Error; err != nil {
		return nil, e.Wrap(err, "error querying error comments")
	}
	_, err := r.isAdminErrorGroupOwner(ctx, errorGroupID)
	if err != nil {
		return nil, e.Wrap(err, "admin is not error group owner")
	}
	if err := r.DB.Delete(&model.ErrorComment{Model: model.Model{ID: id}}).Error; err != nil {
		return nil, e.Wrap(err, "error deleting error_comment")
	}
	return &model.T, nil
}

func (r *mutationResolver) UpdateErrorAlert(ctx context.Context, organizationID int, errorAlertID int, countThreshold int, thresholdWindow int, slackChannels []*modelInputs.SanitizedSlackChannelInput, environments []*string) (*model.ErrorAlert, error) {
	_, err := r.isAdminInOrganization(ctx, organizationID)
	if err != nil {
		return nil, e.Wrap(err, "admin is not in organization")
	}

	alert := &model.ErrorAlert{}
	if err := r.DB.Where(&model.ErrorAlert{Model: model.Model{ID: errorAlertID}}).Find(&alert).Error; err != nil {
		return nil, e.Wrap(err, "error querying error alert")
	}

	sanitizedChannels := []*modelInputs.SanitizedSlackChannel{}
	// For each of the new slack channels, confirm that they exist in the "IntegratedSlackChannels" string.
	for _, ch := range slackChannels {
		sanitizedChannels = append(sanitizedChannels, &modelInputs.SanitizedSlackChannel{WebhookChannel: ch.WebhookChannelName, WebhookChannelID: ch.WebhookChannelID})
	}

	envBytes, err := json.Marshal(environments)
	if err != nil {
		return nil, e.Wrap(err, "error parsing environments")
	}
	envString := string(envBytes)

	channelsBytes, err := json.Marshal(sanitizedChannels)
	if err != nil {
		return nil, e.Wrap(err, "error parsing channels")
	}
	channelsString := string(channelsBytes)

	alert.ChannelsToNotify = &channelsString
	alert.ExcludedEnvironments = &envString
	alert.CountThreshold = countThreshold
	alert.ThresholdWindow = &thresholdWindow
	if err := r.DB.Model(&model.ErrorAlert{
		Alert: model.Alert{
			OrganizationID: organizationID,
		},
		Model: model.Model{
			ID: errorAlertID,
		},
	}).Updates(alert).Error; err != nil {
		return nil, e.Wrap(err, "error updating org fields")
	}
	return alert, nil
}

func (r *mutationResolver) UpdateNewUserAlert(ctx context.Context, organizationID int, sessionAlertID int, countThreshold int, slackChannels []*modelInputs.SanitizedSlackChannelInput, environments []*string) (*model.SessionAlert, error) {
	_, err := r.isAdminInOrganization(ctx, organizationID)
	if err != nil {
		return nil, e.Wrap(err, "admin is not in organization")
	}

	alert := &model.SessionAlert{}
	if err := r.DB.Where(&model.SessionAlert{Model: model.Model{ID: sessionAlertID}}).Where("type IS NULL OR type=?", model.AlertType.NEW_USER).Find(&alert).Error; err != nil {
		return nil, e.Wrap(err, "error querying session alert")
	}

	var sanitizedChannels []*modelInputs.SanitizedSlackChannel
	// For each of the new slack channels, confirm that they exist in the "IntegratedSlackChannels" string.
	for _, ch := range slackChannels {
		sanitizedChannels = append(sanitizedChannels, &modelInputs.SanitizedSlackChannel{WebhookChannel: ch.WebhookChannelName, WebhookChannelID: ch.WebhookChannelID})
	}

	envBytes, err := json.Marshal(environments)
	if err != nil {
		return nil, e.Wrap(err, "error parsing environments")
	}
	envString := string(envBytes)

	channelsBytes, err := json.Marshal(sanitizedChannels)
	if err != nil {
		return nil, e.Wrap(err, "error parsing channels")
	}
	channelsString := string(channelsBytes)

	alert.ChannelsToNotify = &channelsString
	alert.ExcludedEnvironments = &envString
	alert.CountThreshold = countThreshold
	if err := r.DB.Model(&model.SessionAlert{
		Alert: model.Alert{
			OrganizationID: organizationID,
		},
		Model: model.Model{
			ID: sessionAlertID,
		},
	}).Updates(alert).Error; err != nil {
		return nil, e.Wrap(err, "error updating org fields")
	}
	return alert, nil
}

func (r *mutationResolver) UpdateTrackPropertiesAlert(ctx context.Context, organizationID int, sessionAlertID int, slackChannels []*modelInputs.SanitizedSlackChannelInput, environments []*string, trackProperties []*modelInputs.TrackPropertyInput) (*model.SessionAlert, error) {
	_, err := r.isAdminInOrganization(ctx, organizationID)
	if err != nil {
		return nil, e.Wrap(err, "admin is not in organization")
	}

	envBytes, err := json.Marshal(environments)
	if err != nil {
		return nil, e.Wrap(err, "error parsing environments for track properties alert")
	}
	envString := string(envBytes)

	var sanitizedChannels []*modelInputs.SanitizedSlackChannel
	// For each of the new slack channels, confirm that they exist in the "IntegratedSlackChannels" string.
	for _, ch := range slackChannels {
		sanitizedChannels = append(sanitizedChannels, &modelInputs.SanitizedSlackChannel{WebhookChannel: ch.WebhookChannelName, WebhookChannelID: ch.WebhookChannelID})
	}

	channelsBytes, err := json.Marshal(sanitizedChannels)
	if err != nil {
		return nil, e.Wrap(err, "error parsing channels for track properties alert")
	}
	channelsString := string(channelsBytes)

	trackPropertiesBytes, err := json.Marshal(trackProperties)
	if err != nil {
		return nil, e.Wrap(err, "error parsing track properties for track properties alert")
	}
	trackPropertiesString := string(trackPropertiesBytes)

	alert := &model.SessionAlert{}
	alert.ExcludedEnvironments = &envString
	alert.ChannelsToNotify = &channelsString
	alert.TrackProperties = &trackPropertiesString
	if err := r.DB.Model(&model.SessionAlert{
		Alert: model.Alert{
			OrganizationID: organizationID,
		},
		Model: model.Model{
			ID: sessionAlertID,
		},
	}).Updates(alert).Error; err != nil {
		return nil, e.Wrap(err, "error updating org fields for track properties alert")
	}
	return alert, nil
}

func (r *mutationResolver) UpdateUserPropertiesAlert(ctx context.Context, organizationID int, sessionAlertID int, slackChannels []*modelInputs.SanitizedSlackChannelInput, environments []*string, userProperties []*modelInputs.UserPropertyInput) (*model.SessionAlert, error) {
	_, err := r.isAdminInOrganization(ctx, organizationID)
	if err != nil {
		return nil, e.Wrap(err, "admin is not in organization")
	}

	envBytes, err := json.Marshal(environments)
	if err != nil {
		return nil, e.Wrap(err, "error parsing environments for user properties alert")
	}
	envString := string(envBytes)

	var sanitizedChannels []*modelInputs.SanitizedSlackChannel
	// For each of the new slack channels, confirm that they exist in the "IntegratedSlackChannels" string.
	for _, ch := range slackChannels {
		sanitizedChannels = append(sanitizedChannels, &modelInputs.SanitizedSlackChannel{WebhookChannel: ch.WebhookChannelName, WebhookChannelID: ch.WebhookChannelID})
	}

	channelsBytes, err := json.Marshal(sanitizedChannels)
	if err != nil {
		return nil, e.Wrap(err, "error parsing channels for user properties alert")
	}
	channelsString := string(channelsBytes)

	userPropertiesBytes, err := json.Marshal(userProperties)
	if err != nil {
		return nil, e.Wrap(err, "error parsing user properties for user properties alert")
	}
	userPropertiesString := string(userPropertiesBytes)

	alert := &model.SessionAlert{}
	alert.ExcludedEnvironments = &envString
	alert.ChannelsToNotify = &channelsString
	alert.UserProperties = &userPropertiesString
	if err := r.DB.Model(&model.SessionAlert{
		Alert: model.Alert{
			OrganizationID: organizationID,
		},
		Model: model.Model{
			ID: sessionAlertID,
		},
	}).Updates(alert).Error; err != nil {
		return nil, e.Wrap(err, "error updating org fields for user properties alert")
	}
	return alert, nil
}

func (r *mutationResolver) UpdateSessionIsPublic(ctx context.Context, sessionID int, isPublic bool) (*model.Session, error) {
	session, err := r.canAdminModifySession(ctx, sessionID)
	if err != nil {
		return nil, e.Wrap(err, "admin not session owner")
	}
	if err := r.DB.Model(session).Updates(&model.Session{
		IsPublic: &isPublic,
	}).Error; err != nil {
		return nil, e.Wrap(err, "error updating session is_public")
	}

	return session, nil
}

func (r *queryResolver) Session(ctx context.Context, id int) (*model.Session, error) {
	if _, err := r.canAdminViewSession(ctx, id); err != nil {
		return nil, e.Wrap(err, "admin not session owner")
	}
	sessionObj := &model.Session{}
	res := r.DB.Preload("Fields").Where(&model.Session{Model: model.Model{ID: id}}).First(&sessionObj)
	if res.Error != nil {
		return nil, fmt.Errorf("error reading from session: %v", res.Error)
	}
	return sessionObj, nil
}

func (r *queryResolver) Events(ctx context.Context, sessionID int) ([]interface{}, error) {
	if os.Getenv("ENVIRONMENT") == "dev" && sessionID == 1 {
		file, err := ioutil.ReadFile("./tmp/events.json")
		if err != nil {
			return nil, e.Wrap(err, "Failed to read temp file")
		}
		var data []interface{}

		if err := json.Unmarshal([]byte(file), &data); err != nil {
			return nil, e.Wrap(err, "Failed to unmarshal data from file")
		}
		return data, nil
	}
	s, err := r.canAdminViewSession(ctx, sessionID)
	if err != nil {
		return nil, e.Wrap(err, "admin not session owner")
	}
	if en := s.ObjectStorageEnabled; en != nil && *en {
		objectStorageSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
			tracer.ResourceName("db.objectStorageQuery"), tracer.Tag("org_id", s.OrganizationID))
		defer objectStorageSpan.Finish()
		ret, err := r.StorageClient.ReadSessionsFromS3(sessionID, s.OrganizationID)
		if err != nil {
			return nil, err
		}
		return ret, nil
	}
	eventsQuerySpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
		tracer.ResourceName("db.eventsObjectsQuery"), tracer.Tag("org_id", s.OrganizationID))
	eventObjs := []*model.EventsObject{}
	if res := r.DB.Order("created_at desc").Where(&model.EventsObject{SessionID: sessionID}).Find(&eventObjs); res.Error != nil {
		return nil, fmt.Errorf("error reading from events: %v", res.Error)
	}
	eventsQuerySpan.Finish()
	eventsParseSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
		tracer.ResourceName("parse.eventsObjects"), tracer.Tag("org_id", s.OrganizationID))
	allEvents := make(map[string][]interface{})
	for _, eventObj := range eventObjs {
		subEvents := make(map[string][]interface{})
		if err := json.Unmarshal([]byte(eventObj.Events), &subEvents); err != nil {
			return nil, fmt.Errorf("error decoding event data: %v", err)
		}
		allEvents["events"] = append(subEvents["events"], allEvents["events"]...)
	}
	eventsParseSpan.Finish()
	return allEvents["events"], nil
}

func (r *queryResolver) ErrorGroups(ctx context.Context, organizationID int, count int, params *modelInputs.ErrorSearchParamsInput) (*model.ErrorResults, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}
	errorFieldIds := []int{}
	errorFieldQuery := r.DB.Model(&model.ErrorField{})

	if params.Browser != nil {
		errorFieldQuery = errorFieldQuery.Where("name = ? AND value = ?", "browser", params.Browser)
	}

	if params.Os != nil {
		errorFieldQuery = errorFieldQuery.Where("name = ? AND value = ?", "os_name", params.Os)
	}

	if params.VisitedURL != nil {
		errorFieldQuery = errorFieldQuery.Where("name = ? AND value = ?", "visited_url", params.VisitedURL)
	}

	fieldsSelectStatement := ", array_agg(t.error_field_id) fieldIds"
	joinErrorGroupFieldsStatement := "INNER JOIN error_group_fields t ON e.id=t.error_group_id"

	if params.Browser == nil && params.Os == nil && params.VisitedURL == nil {
		fieldsSelectStatement = ""
		joinErrorGroupFieldsStatement = ""
	} else {
		errorFieldQuerySpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal", tracer.ResourceName("db.errorFieldIds"))
		if err := errorFieldQuery.Pluck("id", &errorFieldIds).Error; err != nil {
			return nil, e.Wrap(err, "error querying error fields")
		}
		errorFieldQuerySpan.Finish()

	}

	errorGroups := []model.ErrorGroup{}
	selectPreamble := `SELECT id, organization_id, event, stack_trace, metadata_log, created_at, deleted_at, updated_at, state`
	countPreamble := `SELECT COUNT(*)`

	queryString := fmt.Sprintf(`FROM (SELECT id, organization_id, event, COALESCE(mapped_stack_trace, stack_trace) as stack_trace, metadata_log, created_at, deleted_at, updated_at, state %s
	FROM error_groups e %s GROUP BY e.id) AS rows `, fieldsSelectStatement, joinErrorGroupFieldsStatement)

	queryString += fmt.Sprintf("WHERE (organization_id = %d) ", organizationID)
	queryString += "AND (deleted_at IS NULL) "

	if len(errorFieldIds) > 0 {
		fieldIdConstructionSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
			tracer.ResourceName("fieldIdConstruction"), tracer.Tag("org_id", organizationID))
		t := strings.Replace(fmt.Sprint(errorFieldIds), " ", ",", -1)
		queryString += fmt.Sprintf("AND (fieldIds && ARRAY%s::integer[])", t)
		fieldIdConstructionSpan.Finish()
	}

	if d := params.DateRange; d != nil {
		queryString += fmt.Sprintf("AND (id IN (SELECT error_group_id FROM error_objects WHERE (organization_id=%d) AND (deleted_at IS NULL) AND (created_at > '%s') AND (created_at < '%s')))", organizationID, d.StartDate.Format("2006-01-02 15:04:05"), d.EndDate.Format("2006-01-02 15:04:05"))
	}

	if resolved := params.HideResolved; resolved != nil && *resolved {
		queryString += fmt.Sprintf("AND (state <> '%s') ", model.ErrorGroupStates.RESOLVED)
	}

	if params.Event != nil {
		queryString += fmt.Sprintf("AND (event ILIKE '%s') ", "%"+*params.Event+"%")
	}

	var g errgroup.Group
	var queriedErrorGroupsCount int64

	g.Go(func() error {
		errorGroupSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
			tracer.ResourceName("db.errorGroups"), tracer.Tag("org_id", organizationID))
		if err := r.DB.Raw(fmt.Sprintf("%s %s ORDER BY updated_at DESC LIMIT %d", selectPreamble, queryString, count)).Scan(&errorGroups).Error; err != nil {
			return e.Wrap(err, "error reading from error groups")
		}
		errorGroupSpan.Finish()
		return nil
	})

	g.Go(func() error {
		errorGroupCountSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
			tracer.ResourceName("db.errorGroupsCount"), tracer.Tag("org_id", organizationID))
		if err := r.DB.Raw(fmt.Sprintf("%s %s", countPreamble, queryString)).Scan(&queriedErrorGroupsCount).Error; err != nil {
			return e.Wrap(err, "error counting error groups")
		}

		errorGroupCountSpan.Finish()
		return nil
	})

	// Waits for both goroutines to finish, then returns the first non-nil error (if any).
	if err := g.Wait(); err != nil {
		return nil, e.Wrap(err, "error querying error groups data")
	}

	errorResults := &model.ErrorResults{
		ErrorGroups: errorGroups,
		TotalCount:  queriedErrorGroupsCount,
	}
	return errorResults, nil
}

func (r *queryResolver) ErrorGroup(ctx context.Context, id int) (*model.ErrorGroup, error) {
	return r.isAdminErrorGroupOwner(ctx, id)
}

func (r *queryResolver) Messages(ctx context.Context, sessionID int) ([]interface{}, error) {
	s, err := r.canAdminViewSession(ctx, sessionID)
	if err != nil {
		return nil, e.Wrap(err, "admin not session owner")
	}
	if en := s.ObjectStorageEnabled; en != nil && *en {
		objectStorageSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
			tracer.ResourceName("db.objectStorageQuery"), tracer.Tag("org_id", s.OrganizationID))
		defer objectStorageSpan.Finish()
		ret, err := r.StorageClient.ReadMessagesFromS3(sessionID, s.OrganizationID)
		if err != nil {
			return nil, e.Wrap(err, "error pulling messages from s3")
		}
		return ret, nil
	}
	messagesObj := []*model.MessagesObject{}
	if res := r.DB.Order("created_at desc").Where(&model.MessagesObject{SessionID: sessionID}).Find(&messagesObj); res.Error != nil {
		return nil, fmt.Errorf("error reading from messages: %v", res.Error)
	}
	allEvents := make(map[string][]interface{})
	for _, messageObj := range messagesObj {
		subMessage := make(map[string][]interface{})
		if err := json.Unmarshal([]byte(messageObj.Messages), &subMessage); err != nil {
			return nil, fmt.Errorf("error decoding message data: %v", err)
		}
		allEvents["messages"] = append(subMessage["messages"], allEvents["messages"]...)
	}
	return allEvents["messages"], nil
}

func (r *queryResolver) Errors(ctx context.Context, sessionID int) ([]*model.ErrorObject, error) {
	s, err := r.canAdminViewSession(ctx, sessionID)
	if err != nil {
		return nil, e.Wrap(err, "admin not session owner")
	}
	eventsQuerySpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
		tracer.ResourceName("db.errorObjectsQuery"), tracer.Tag("org_id", s.OrganizationID))
	defer eventsQuerySpan.Finish()
	errorsObj := []*model.ErrorObject{}
	if res := r.DB.Order("created_at asc").Where(&model.ErrorObject{SessionID: sessionID}).Find(&errorsObj); res.Error != nil {
		return nil, fmt.Errorf("error reading from errors: %v", res.Error)
	}
	return errorsObj, nil
}

func (r *queryResolver) Resources(ctx context.Context, sessionID int) ([]interface{}, error) {
	s, err := r.canAdminViewSession(ctx, sessionID)
	if err != nil {
		return nil, e.Wrap(err, "admin not session owner")
	}
	if en := s.ObjectStorageEnabled; en != nil && *en {
		objectStorageSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
			tracer.ResourceName("db.objectStorageQuery"), tracer.Tag("org_id", s.OrganizationID))
		defer objectStorageSpan.Finish()
		ret, err := r.StorageClient.ReadResourcesFromS3(sessionID, s.OrganizationID)
		if err != nil {
			return nil, e.Wrap(err, "error pulling resources from s3")
		}
		return ret, nil
	}
	resourcesObject := []*model.ResourcesObject{}
	if res := r.DB.Order("created_at desc").Where(&model.ResourcesObject{SessionID: sessionID}).Find(&resourcesObject); res.Error != nil {
		return nil, fmt.Errorf("error reading from resources: %v", res.Error)
	}
	allResources := make(map[string][]interface{})
	for _, resourceObj := range resourcesObject {
		subResources := make(map[string][]interface{})
		if err := json.Unmarshal([]byte(resourceObj.Resources), &subResources); err != nil {
			return nil, fmt.Errorf("error decoding resource data: %v", err)
		}
		allResources["resources"] = append(subResources["resources"], allResources["resources"]...)
	}
	return allResources["resources"], nil
}

func (r *queryResolver) SessionComments(ctx context.Context, sessionID int) ([]*model.SessionComment, error) {
	if _, err := r.canAdminViewSession(ctx, sessionID); err != nil {
		return nil, e.Wrap(err, "admin not session owner")
	}

	sessionComments := []*model.SessionComment{}
	if err := r.DB.Where(model.SessionComment{SessionId: sessionID}).Order("timestamp asc").Find(&sessionComments).Error; err != nil {
		return nil, e.Wrap(err, "error querying session comments for session")
	}
	return sessionComments, nil
}

func (r *queryResolver) SessionCommentsForAdmin(ctx context.Context) ([]*model.SessionComment, error) {
	admin, err := r.getCurrentAdmin(ctx)
	if err != nil {
		return nil, e.Wrap(err, "error retrieving user")
	}
	var sessionComments []*model.SessionComment
	if err := r.DB.Model(admin).Association("SessionComments").Find(&sessionComments); err != nil {
		return nil, e.Wrap(err, "error getting session comments for")
	}

	return sessionComments, nil
}

func (r *queryResolver) SessionCommentsForOrganization(ctx context.Context, organizationID int) ([]*model.SessionComment, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org for session comments")
	}

	var sessionComments []*model.SessionComment
	if err := r.DB.Where(model.SessionComment{OrganizationID: organizationID}).Find(&sessionComments).Error; err != nil {
		return nil, e.Wrap(err, "error getting session comments for organization")
	}

	return sessionComments, nil
}

func (r *queryResolver) ErrorComments(ctx context.Context, errorGroupID int) ([]*model.ErrorComment, error) {
	if _, err := r.isAdminErrorGroupOwner(ctx, errorGroupID); err != nil {
		return nil, e.Wrap(err, "admin not error owner")
	}

	errorComments := []*model.ErrorComment{}
	if err := r.DB.Where(model.ErrorComment{ErrorId: errorGroupID}).Order("created_at asc").Find(&errorComments).Error; err != nil {
		return nil, e.Wrap(err, "error querying error comments for error_group")
	}
	return errorComments, nil
}

func (r *queryResolver) ErrorCommentsForAdmin(ctx context.Context) ([]*model.ErrorComment, error) {
	admin, err := r.getCurrentAdmin(ctx)
	if err != nil {
		return nil, e.Wrap(err, "error retrieving user")
	}
	var errorComments []*model.ErrorComment
	if err := r.DB.Model(admin).Association("ErrorComments").Find(&errorComments); err != nil {
		return nil, e.Wrap(err, "error getting error comments for admin")
	}

	return errorComments, nil
}

func (r *queryResolver) ErrorCommentsForOrganization(ctx context.Context, organizationID int) ([]*model.ErrorComment, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org for error comments")
	}

	var errorComments []*model.ErrorComment
	if err := r.DB.Where(model.ErrorComment{OrganizationID: organizationID}).Find(&errorComments).Error; err != nil {
		return nil, e.Wrap(err, "error getting error comments for organization")
	}

	return errorComments, nil
}

func (r *queryResolver) Admins(ctx context.Context, organizationID int) ([]*model.Admin, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}
	admins := []*model.Admin{}
	err := r.DB.Model(
		&model.Organization{Model: model.Model{ID: organizationID}}).Order("created_at asc").Association("Admins").Find(&admins)
	if err != nil {
		return nil, e.Wrap(err, "error getting associated admins")
	}
	return admins, nil
}

func (r *queryResolver) IsIntegrated(ctx context.Context, organizationID int) (*bool, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}
	var count int64
	err := r.DB.Model(&model.Session{}).Where(
		&model.Session{OrganizationID: organizationID}).Count(&count).Error
	if err != nil {
		return nil, e.Wrap(err, "error getting associated admins")
	}
	if count > 0 {
		return &model.T, nil
	}
	return &model.F, nil
}

func (r *queryResolver) UnprocessedSessionsCount(ctx context.Context, organizationID int) (*int64, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}

	var count int64
	if err := r.DB.Model(&model.Session{}).Where(&model.Session{OrganizationID: organizationID, Processed: &model.F}).Count(&count).Error; err != nil {
		return nil, e.Wrap(err, "error retrieving count of unprocessed sessions")
	}

	return &count, nil
}

func (r *queryResolver) AdminHasCreatedComment(ctx context.Context, adminID int) (*bool, error) {
	if err := r.DB.Model(&model.SessionComment{}).Where(&model.SessionComment{
		AdminId: adminID,
	}).First(&model.SessionComment{}).Error; err != nil {
		return &model.F, nil
	}

	return &model.T, nil
}

func (r *queryResolver) OrganizationHasViewedASession(ctx context.Context, organizationID int) (*model.Session, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}

	session := model.Session{}
	if err := r.DB.Model(&session).Where(&model.Session{OrganizationID: organizationID, Viewed: &model.T}).First(&session).Error; err != nil {
		return &session, nil
	}
	return &session, nil
}

func (r *queryResolver) DailySessionsCount(ctx context.Context, organizationID int, dateRange modelInputs.DateRangeInput) ([]*model.DailySessionCount, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}

	dailySessions := []*model.DailySessionCount{}

	startDateUTC := time.Date(dateRange.StartDate.UTC().Year(), dateRange.StartDate.UTC().Month(), dateRange.StartDate.UTC().Day(), 0, 0, 0, 0, time.UTC)
	endDateUTC := time.Date(dateRange.EndDate.UTC().Year(), dateRange.EndDate.UTC().Month(), dateRange.EndDate.UTC().Day(), 0, 0, 0, 0, time.UTC)

	if err := r.DB.Where(&model.DailySessionCount{OrganizationID: organizationID}).Where("date BETWEEN ? AND ?", startDateUTC, endDateUTC).Find(&dailySessions).Error; err != nil {
		return nil, e.Wrap(err, "error reading from daily sessions")
	}

	return dailySessions, nil
}

func (r *queryResolver) DailyErrorsCount(ctx context.Context, organizationID int, dateRange modelInputs.DateRangeInput) ([]*model.DailyErrorCount, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}

	dailyErrors := []*model.DailyErrorCount{}

	startDateUTC := time.Date(dateRange.StartDate.UTC().Year(), dateRange.StartDate.UTC().Month(), dateRange.StartDate.UTC().Day(), 0, 0, 0, 0, time.UTC)
	endDateUTC := time.Date(dateRange.EndDate.UTC().Year(), dateRange.EndDate.UTC().Month(), dateRange.EndDate.UTC().Day(), 0, 0, 0, 0, time.UTC)

	if err := r.DB.Where(&model.DailyErrorCount{OrganizationID: organizationID}).Where("date BETWEEN ? AND ?", startDateUTC, endDateUTC).Find(&dailyErrors).Error; err != nil {
		return nil, e.Wrap(err, "error reading from daily errors")
	}

	return dailyErrors, nil
}

func (r *queryResolver) Referrers(ctx context.Context, organizationID int, lookBackPeriod int) ([]*modelInputs.ReferrerTablePayload, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}

	referrers := []*modelInputs.ReferrerTablePayload{}

	if err := r.DB.Raw(fmt.Sprintf("SELECT DISTINCT(value) as host, COUNT(value), count(value) * 100.0 / (select count(*) from fields where name='referrer' and organization_id=%d and created_at >= NOW() - INTERVAL '%d DAY') as percent FROM (SELECT SUBSTRING(value from '(?:.*://)?(?:www\\.)?([^/]*)') AS value FROM fields WHERE name='referrer' AND organization_id=%d AND created_at >= NOW() - INTERVAL '%d DAY') t1 GROUP BY value ORDER BY count desc LIMIT 200", organizationID, lookBackPeriod, organizationID, lookBackPeriod)).Scan(&referrers).Error; err != nil {
		return nil, e.Wrap(err, "error getting referrers")
	}

	return referrers, nil
}

func (r *queryResolver) NewUsersCount(ctx context.Context, organizationID int, lookBackPeriod int) (*modelInputs.NewUsersCount, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}

	var count int64
	if err := r.DB.Raw(fmt.Sprintf("SELECT COUNT(*) FROM sessions WHERE organization_id=%d AND first_time=true AND created_at >= NOW() - INTERVAL '%d DAY'", organizationID, lookBackPeriod)).Scan(&count).Error; err != nil {
		return nil, e.Wrap(err, "error retrieving count of first time users")
	}

	return &modelInputs.NewUsersCount{Count: count}, nil
}

func (r *queryResolver) TopUsers(ctx context.Context, organizationID int, lookBackPeriod int) ([]*modelInputs.TopUsersPayload, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}

	var topUsersPayload = []*modelInputs.TopUsersPayload{}
	topUsersSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
		tracer.ResourceName("db.topUsers"), tracer.Tag("org_id", organizationID))
	if err := r.DB.Raw(fmt.Sprintf(`SELECT identifier, (SELECT id FROM fields WHERE organization_id=%d AND type='user' AND name='identifier' AND value=identifier) AS id, SUM(active_length) as total_active_time, SUM(active_length) / (SELECT SUM(active_length) from sessions WHERE active_length IS NOT NULL AND organization_id=%d AND identifier <> '' AND created_at >= NOW() - INTERVAL '%d DAY' AND processed=true) as active_time_percentage
	FROM (SELECT identifier, active_length from sessions WHERE active_length IS NOT NULL AND organization_id=%d AND identifier <> '' AND created_at >= NOW() - INTERVAL '%d DAY' AND processed=true) q1
	GROUP BY identifier
	ORDER BY total_active_time DESC
	LIMIT 50`, organizationID, organizationID, lookBackPeriod, organizationID, lookBackPeriod)).Scan(&topUsersPayload).Error; err != nil {
		return nil, e.Wrap(err, "error retrieving top users")
	}
	topUsersSpan.Finish()

	return topUsersPayload, nil
}

func (r *queryResolver) AverageSessionLength(ctx context.Context, organizationID int, lookBackPeriod int) (*modelInputs.AverageSessionLength, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}
	var length float64
	if err := r.DB.Raw(fmt.Sprintf("SELECT avg(active_length) FROM sessions WHERE organization_id=%d AND processed=true AND active_length IS NOT NULL AND created_at >= NOW() - INTERVAL '%d DAY';", organizationID, lookBackPeriod)).Scan(&length).Error; err != nil {
		return nil, e.Wrap(err, "error retrieving average length for sessions")
	}

	return &modelInputs.AverageSessionLength{Length: length}, nil
}

func (r *queryResolver) UserFingerprintCount(ctx context.Context, organizationID int, lookBackPeriod int) (*modelInputs.UserFingerprintCount, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}

	var count int64
	span, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
		tracer.ResourceName("db.userFingerprintCount"), tracer.Tag("org_id", organizationID))
	if err := r.DB.Raw(fmt.Sprintf("SELECT count(DISTINCT fingerprint) from sessions WHERE identifier='' AND fingerprint IS NOT NULL AND created_at >= NOW() - INTERVAL '%d DAY' AND organization_id=%d AND length >= 1000;", lookBackPeriod, organizationID)).Scan(&count).Error; err != nil {
		return nil, e.Wrap(err, "error retrieving user fingerprint count")
	}
	span.Finish()

	return &modelInputs.UserFingerprintCount{Count: count}, nil
}

func (r *queryResolver) Sessions(ctx context.Context, organizationID int, count int, lifecycle modelInputs.SessionLifecycle, starred bool, params *modelInputs.SearchParamsInput) (*model.SessionResults, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}
	// Find fields based on the search params
	//included fields
	fieldCheck := true
	visitedCheck := true
	referrerCheck := true
	fieldIds := []int{}
	fieldQuery := r.DB.Model(&model.Field{})
	visitedIds := []int{}
	referrerIds := []int{}
	visitedQuery := r.DB.Model(&model.Field{})
	referrerQuery := r.DB.Model(&model.Field{})

	fieldsSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
		tracer.ResourceName("db.fieldsQuery"), tracer.Tag("org_id", organizationID))
	for _, prop := range params.UserProperties {
		if prop.Name == "contains" {
			fieldQuery = fieldQuery.Or("value ILIKE ? and type = ?", "%"+prop.Value+"%", "user")
		} else if prop.ID == nil || *prop.ID == 0 {
			fieldQuery = fieldQuery.Or("name = ? AND value = ? AND type = ?", prop.Name, prop.Value, "user")
		} else {
			fieldIds = append(fieldIds, *prop.ID)
		}
	}

	for _, prop := range params.TrackProperties {
		if prop.Name == "contains" {
			fieldQuery = fieldQuery.Or("value ILIKE ? and type = ?", "%"+prop.Value+"%", "track")
		} else if prop.ID == nil || *prop.ID == 0 {
			fieldQuery = fieldQuery.Or("name = ? AND value = ? AND type = ?", prop.Name, prop.Value, "track")
		} else {
			fieldIds = append(fieldIds, *prop.ID)
		}
	}

	if params.VisitedURL != nil {
		visitedQuery = visitedQuery.Or("name = ? and value ILIKE ?", "visited-url", "%"+*params.VisitedURL+"%")
	}

	if params.Referrer != nil {
		referrerQuery = referrerQuery.Or("name = ? and value ILIKE ?", "referrer", "%"+*params.Referrer+"%")
	}

	if len(params.UserProperties)+len(params.TrackProperties) > 0 {
		if len(params.UserProperties)+len(params.TrackProperties) != len(fieldIds) {
			var tempFieldIds []int
			if err := fieldQuery.Pluck("id", &tempFieldIds).Error; err != nil {
				return nil, e.Wrap(err, "error querying initial set of session fields")
			}
			fieldIds = append(fieldIds, tempFieldIds...)
		}
		if len(fieldIds) == 0 {
			fieldCheck = false
		}
	}

	if params.VisitedURL != nil {
		if err := visitedQuery.Pluck("id", &visitedIds).Error; err != nil {
			return nil, e.Wrap(err, "error querying visited-url fields")
		}
		if len(visitedIds) == 0 {
			visitedCheck = false
		}
	}

	if params.Referrer != nil {
		if err := referrerQuery.Pluck("id", &referrerIds).Error; err != nil {
			return nil, e.Wrap(err, "error querying referrer fields")
		}
		if len(referrerIds) == 0 {
			referrerCheck = false
		}
	}

	//excluded fields
	notFieldIds := []int{}
	notFieldQuery := r.DB.Model(&model.Field{})

	for _, prop := range params.ExcludedProperties {
		if prop.Name == "contains" {
			notFieldQuery = notFieldQuery.Or("value ILIKE ? and type = ?", "%"+prop.Value+"%", "user")
		} else if prop.ID == nil || *prop.ID == 0 {
			notFieldQuery = notFieldQuery.Or("name = ? AND value = ? AND type = ?", prop.Name, prop.Value, "user")
		} else {
			notFieldIds = append(notFieldIds, *prop.ID)
		}
	}

	if len(params.ExcludedProperties) != len(notFieldIds) {
		var tempNotFieldIds []int
		if err := notFieldQuery.Pluck("id", &notFieldIds).Error; err != nil {
			return nil, e.Wrap(err, "error querying initial set of excluded sessions fields")
		}
		notFieldIds = append(notFieldIds, tempNotFieldIds...)
	}

	//excluded track fields
	notTrackFieldIds := []int{}
	notTrackFieldQuery := r.DB.Model(&model.Field{})

	for _, prop := range params.ExcludedTrackProperties {
		if prop.Name == "contains" {
			notTrackFieldQuery = notTrackFieldQuery.Or("value ILIKE ? and type = ?", "%"+prop.Value+"%", "track")
		} else if prop.ID == nil || *prop.ID == 0 {
			notTrackFieldQuery = notTrackFieldQuery.Or("name = ? AND value = ? AND type = ?", prop.Name, prop.Value, "track")
		} else {
			notTrackFieldIds = append(notTrackFieldIds, *prop.ID)
		}
	}

	//pluck not field ids
	if len(params.ExcludedTrackProperties) != len(notTrackFieldIds) {
		var tempNotTrackFieldIds []int
		if err := notTrackFieldQuery.Pluck("id", &tempNotTrackFieldIds).Error; err != nil {
			return nil, e.Wrap(err, "error querying initial set of excluded track sessions fields")
		}
		notTrackFieldIds = append(notTrackFieldIds, tempNotTrackFieldIds...)
	}

	fieldsSpan.Finish()

	sessionsQueryPreamble := "SELECT id, user_id, organization_id, processed, starred, first_time, os_name, os_version, browser_name, browser_version, city, state, postal, identifier, fingerprint, created_at, deleted_at, length, active_length, user_object, viewed"
	fieldsInnerJoinStatement := "INNER JOIN session_fields t ON s.id=t.session_id"
	fieldsSelectStatement := ", array_agg(t.field_id) fieldIds"
	isUnfilteredQuery := len(fieldIds) == 0 && len(visitedIds) == 0 && len(referrerIds) == 0 && len(notFieldIds) == 0 && len(notTrackFieldIds) == 0
	if isUnfilteredQuery {
		fieldsInnerJoinStatement = ""
		fieldsSelectStatement = ""
	}
	joinClause := fmt.Sprintf("FROM (SELECT id, user_id, organization_id, processed, starred, first_time, os_name, os_version, browser_name, browser_version, city, state, postal, identifier, fingerprint, created_at, deleted_at, length, active_length, user_object, viewed, within_billing_quota %s FROM sessions s %s GROUP BY s.id) AS rows", fieldsSelectStatement, fieldsInnerJoinStatement)
	whereClause := ` `

	whereClause += fmt.Sprintf("WHERE (organization_id = %d) ", organizationID)
	if lifecycle == modelInputs.SessionLifecycleCompleted {
		whereClause += fmt.Sprintf("AND (length > %d) ", 1000)
	}
	if starred {
		whereClause += "AND (starred = true) "
	}
	if firstTime := params.FirstTime; firstTime != nil && *firstTime {
		whereClause += "AND (first_time = true) "
	}
	if params.LengthRange != nil {
		if params.LengthRange.Min != nil {
			whereClause += fmt.Sprintf("AND (active_length >= %d) ", *params.LengthRange.Min*60000)
		}
		if params.LengthRange.Max != nil {
			if *params.LengthRange.Max != 60 && *params.LengthRange.Max != 0 {
				whereClause += fmt.Sprintf("AND (active_length <= %d) ", *params.LengthRange.Max*60000)
			}
		}
	}

	if lifecycle == modelInputs.SessionLifecycleCompleted {
		whereClause += "AND (processed = true) "
	} else if lifecycle == modelInputs.SessionLifecycleLive {
		whereClause += "AND (processed = false) "
	}
	whereClause += "AND (deleted_at IS NULL) "

	if len(fieldIds) > 0 {
		t := strings.Replace(fmt.Sprint(fieldIds), " ", ",", -1)
		whereClause += fmt.Sprintf("AND (fieldIds && ARRAY%s::integer[])", t)
	}

	if len(visitedIds) > 0 {
		t := strings.Replace(fmt.Sprint(visitedIds), " ", ",", -1)
		whereClause += fmt.Sprintf("AND (fieldIds && ARRAY%s::integer[])", t)
	}

	if len(referrerIds) > 0 {
		t := strings.Replace(fmt.Sprint(referrerIds), " ", ",", -1)
		whereClause += fmt.Sprintf("AND (fieldIds && ARRAY%s::integer[])", t)
	}

	if len(notFieldIds) > 0 {
		t := strings.Replace(fmt.Sprint(notFieldIds), " ", ",", -1)
		whereClause += fmt.Sprintf("AND NOT (fieldIds && ARRAY%s::integer[])", t)
	}

	if len(notTrackFieldIds) > 0 {
		t := strings.Replace(fmt.Sprint(notTrackFieldIds), " ", ",", -1)
		whereClause += fmt.Sprintf("AND NOT (fieldIds && ARRAY%s::integer[])", t)
	}

	if d := params.DateRange; d != nil {
		whereClause += fmt.Sprintf("AND (created_at > '%s') AND (created_at < '%s') ", d.StartDate.Format("2006-01-02 15:04:05"), d.EndDate.Format("2006-01-02 15:04:05"))
	}

	if os := params.Os; os != nil {
		whereClause += fmt.Sprintf("AND (os_name = '%s') ", *os)
	}

	if identified := params.Identified; identified != nil && *identified {
		whereClause += "AND (length(identifier) > 0) "
	}

	if viewed := params.HideViewed; viewed != nil && *viewed {
		whereClause += "AND (viewed = false OR viewed IS NULL) "
	}

	if browser := params.Browser; browser != nil {
		whereClause += fmt.Sprintf("AND (browser_name = '%s') ", *browser)
	}

	if deviceId := params.DeviceID; deviceId != nil {
		whereClause += fmt.Sprintf("AND (fingerprint = '%s') ", *deviceId)
	}

	//if there should be fields but aren't no sessions are returned
	if !fieldCheck || !visitedCheck || !referrerCheck {
		whereClause += "AND (id != id) "
	}

	// user shouldn't see sessions that are not within billing quota
	whereClause += "AND (within_billing_quota IS NULL OR within_billing_quota=true) "

	var g errgroup.Group
	queriedSessions := []model.Session{}
	var queriedSessionsCount int64
	whereClauseSuffix := "AND NOT ((processed = true AND ((active_length IS NOT NULL AND active_length < 1000) OR (active_length IS NULL AND length < 1000)))) "
	logTags := []string{fmt.Sprintf("org_id:%d", organizationID), fmt.Sprintf("filtered:%t", !isUnfilteredQuery)}

	g.Go(func() error {
		if params.LengthRange != nil {
			if params.LengthRange.Min != nil || params.LengthRange.Max != nil {
				whereClauseSuffix = "AND processed = true "
			}

		}
		whereClause += whereClauseSuffix
		sessionsSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
			tracer.ResourceName("db.sessionsQuery"), tracer.Tag("org_id", organizationID))
		start := time.Now()
		query := fmt.Sprintf("%s %s %s ORDER BY created_at DESC LIMIT %d", sessionsQueryPreamble, joinClause, whereClause, count)
		if err := r.DB.Raw(query).Scan(&queriedSessions).Error; err != nil {
			return e.Wrap(err, "error querying filtered sessions")
		}
		duration := time.Since(start)
		hlog.Timing("db.sessionsQuery.duration", duration, logTags, 1)
		hlog.Incr("db.sessionsQuery.count", logTags, 1)
		if duration.Milliseconds() > 3000 {
			log.Error(e.New(fmt.Sprintf("sessionsQuery took %dms: %s", duration.Milliseconds(), query)))
		}
		sessionsSpan.Finish()
		return nil
	})

	g.Go(func() error {
		sessionCountSpan, _ := tracer.StartSpanFromContext(ctx, "resolver.internal",
			tracer.ResourceName("db.sessionsCountQuery"), tracer.Tag("org_id", organizationID))
		start := time.Now()
		query := fmt.Sprintf("SELECT count(*) %s %s %s", joinClause, whereClause, whereClauseSuffix)
		if err := r.DB.Raw(query).Scan(&queriedSessionsCount).Error; err != nil {
			return e.Wrap(err, "error querying filtered sessions count")
		}
		duration := time.Since(start)
		hlog.Timing("db.sessionsCountQuery.duration", duration, logTags, 1)
		if duration.Milliseconds() > 3000 {
			log.Error(e.New(fmt.Sprintf("sessionsCountQuery took %dms: %s", duration.Milliseconds(), query)))
		}
		sessionCountSpan.Finish()
		return nil
	})

	// Waits for both goroutines to finish, then returns the first non-nil error (if any).
	if err := g.Wait(); err != nil {
		return nil, e.Wrap(err, "error querying session data")
	}

	sessionList := &model.SessionResults{
		Sessions:   queriedSessions,
		TotalCount: queriedSessionsCount,
	}
	return sessionList, nil
}

func (r *queryResolver) BillingDetails(ctx context.Context, organizationID int) (*modelInputs.BillingDetails, error) {
	org, err := r.isAdminInOrganization(ctx, organizationID)
	if err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}

	stripePriceID := org.StripePriceID
	planType := modelInputs.PlanTypeFree
	if stripePriceID != nil {
		planType = pricing.FromPriceID(*stripePriceID)
	}

	var g errgroup.Group
	var meter int64
	var queriedSessionsOutOfQuota int64

	g.Go(func() error {
		meter, err = pricing.GetOrgQuota(r.DB, organizationID)
		if err != nil {
			return e.Wrap(err, "error from get quota")
		}
		return nil
	})

	g.Go(func() error {
		queriedSessionsOutOfQuota, err = pricing.GetOrgQuotaOverflow(ctx, r.DB, organizationID)
		if err != nil {
			return e.Wrap(err, "error from get quota overflow")
		}
		return nil
	})

	// Waits for both goroutines to finish, then returns the first non-nil error (if any).
	if err := g.Wait(); err != nil {
		return nil, e.Wrap(err, "error querying session data for billing details")
	}

	quota := pricing.TypeToQuota(planType)
	// use monthly session limit if it exists
	if org.MonthlySessionLimit != nil {
		quota = *org.MonthlySessionLimit
	}
	details := &modelInputs.BillingDetails{
		Plan: &modelInputs.Plan{
			Type:  modelInputs.PlanType(planType.String()),
			Quota: quota,
		},
		Meter:              meter,
		SessionsOutOfQuota: queriedSessionsOutOfQuota,
	}
	return details, nil
}

func (r *queryResolver) FieldSuggestion(ctx context.Context, organizationID int, name string, query string) ([]*model.Field, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "error querying organization")
	}
	fields := []*model.Field{}
	res := r.DB.Where(&model.Field{OrganizationID: organizationID, Name: name}).
		Where("length(value) > ?", 0).
		Where("value ILIKE ?", "%"+query+"%").
		Limit(model.SUGGESTION_LIMIT_CONSTANT).
		Find(&fields)
	if err := res.Error; err != nil {
		return nil, e.Wrap(err, "error querying field suggestion")
	}
	return fields, nil
}

func (r *queryResolver) PropertySuggestion(ctx context.Context, organizationID int, query string, typeArg string) ([]*model.Field, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "error querying organization")
	}
	fields := []*model.Field{}
	res := r.DB.Where(&model.Field{OrganizationID: organizationID, Type: typeArg}).Where(r.DB.
		Where(r.DB.Where("length(value) > ?", 0).Where("value ILIKE ?", "%"+query+"%")).
		Or(r.DB.Where("length(name) > ?", 0).Where("name ILIKE ?", "%"+query+"%"))).
		Limit(model.SUGGESTION_LIMIT_CONSTANT).
		Find(&fields)
	if err := res.Error; err != nil {
		return nil, e.Wrap(err, "error querying field suggestion")
	}
	return fields, nil
}

func (r *queryResolver) ErrorFieldSuggestion(ctx context.Context, organizationID int, name string, query string) ([]*model.ErrorField, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "error querying organization")
	}
	fields := []*model.ErrorField{}
	res := r.DB.Where(&model.ErrorField{Name: name}).
		Where("length(value) > ?", 0).
		Where("value ILIKE ?", "%"+query+"%").
		Where("organization_id = ?", organizationID).
		Limit(model.SUGGESTION_LIMIT_CONSTANT).
		Find(&fields)
	if err := res.Error; err != nil {
		return nil, e.Wrap(err, "error querying error field suggestion")
	}
	return fields, nil
}

func (r *queryResolver) Organizations(ctx context.Context) ([]*model.Organization, error) {
	admin, err := r.getCurrentAdmin(ctx)
	if err != nil {
		return nil, e.Wrap(err, "error retrieiving user")
	}
	orgs := []*model.Organization{}
	if err := r.DB.Model(&admin).Association("Organizations").Find(&orgs); err != nil {
		return nil, e.Wrap(err, "error getting associated organizations")
	}
	return orgs, nil
}

func (r *queryResolver) ErrorAlert(ctx context.Context, organizationID int) (*model.ErrorAlert, error) {
	_, err := r.isAdminInOrganization(ctx, organizationID)
	if err != nil {
		return nil, e.Wrap(err, "error querying organization")
	}
	alert := model.ErrorAlert{}
	if err := r.DB.Where(&model.ErrorAlert{Alert: model.Alert{OrganizationID: organizationID}}).First(&alert).Error; err != nil {
		return nil, e.Wrap(err, "error querying error alerts")
	}
	return &alert, nil
}

func (r *queryResolver) NewUserAlert(ctx context.Context, organizationID int) (*model.SessionAlert, error) {
	_, err := r.isAdminInOrganization(ctx, organizationID)
	if err != nil {
		return nil, e.Wrap(err, "error querying organization on new user alert")
	}
	var alert model.SessionAlert
	if err := r.DB.Where(&model.SessionAlert{Alert: model.Alert{OrganizationID: organizationID}}).Where("type IS NULL OR type=?", model.AlertType.NEW_USER).First(&alert).Error; err != nil {
		return nil, e.Wrap(err, "error querying  new user alert")
	}
	return &alert, nil
}

func (r *queryResolver) TrackPropertiesAlert(ctx context.Context, organizationID int) (*model.SessionAlert, error) {
	_, err := r.isAdminInOrganization(ctx, organizationID)
	if err != nil {
		return nil, e.Wrap(err, "error querying organization")
	}
	var alert model.SessionAlert
	if err := r.DB.Where(&model.SessionAlert{Alert: model.Alert{OrganizationID: organizationID, Type: &model.AlertType.TRACK_PROPERTIES}}).First(&alert).Error; err != nil {
		return nil, e.Wrap(err, "error querying track properties alert")
	}
	return &alert, nil
}

func (r *queryResolver) UserPropertiesAlert(ctx context.Context, organizationID int) (*model.SessionAlert, error) {
	_, err := r.isAdminInOrganization(ctx, organizationID)
	if err != nil {
		return nil, e.Wrap(err, "error querying organization")
	}
	var alert model.SessionAlert
	if err := r.DB.Where(&model.SessionAlert{Alert: model.Alert{OrganizationID: organizationID}}).Where("type=?", model.AlertType.USER_PROPERTIES).First(&alert).Error; err != nil {
		return nil, e.Wrap(err, "error querying user properties alert")
	}
	return &alert, nil
}

func (r *queryResolver) OrganizationSuggestion(ctx context.Context, query string) ([]*model.Organization, error) {
	orgs := []*model.Organization{}
	if r.isWhitelistedAccount(ctx) {
		if err := r.DB.Model(&model.Organization{}).Where("name ILIKE ?", "%"+query+"%").Find(&orgs).Error; err != nil {
			return nil, e.Wrap(err, "error getting associated organizations")
		}
	}
	return orgs, nil
}

func (r *queryResolver) EnvironmentSuggestion(ctx context.Context, query string, organizationID int) ([]*model.Field, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "error querying organization")
	}
	fields := []*model.Field{}
	res := r.DB.Where(&model.Field{OrganizationID: organizationID, Type: "session", Name: "environment"}).
		Where("length(value) > ?", 0).
		Where("value ILIKE ?", "%"+query+"%").
		Limit(model.SUGGESTION_LIMIT_CONSTANT).
		Find(&fields)
	if err := res.Error; err != nil {
		return nil, e.Wrap(err, "error querying field suggestion")
	}
	return fields, nil
}

func (r *queryResolver) SlackChannelSuggestion(ctx context.Context, organizationID int) ([]*modelInputs.SanitizedSlackChannel, error) {
	org, err := r.isAdminInOrganization(ctx, organizationID)
	if err != nil {
		return nil, e.Wrap(err, "error getting org")
	}
	chs, err := org.IntegratedSlackChannels()
	if err != nil {
		return nil, e.Wrap(err, "error retrieiving existing channels")
	}
	ret := []*modelInputs.SanitizedSlackChannel{}
	for _, ch := range chs {
		channel := ch.WebhookChannel
		channelID := ch.WebhookChannelID
		ret = append(ret, &modelInputs.SanitizedSlackChannel{
			WebhookChannel:   &channel,
			WebhookChannelID: &channelID,
		})
	}
	return ret, nil
}

func (r *queryResolver) Organization(ctx context.Context, id int) (*model.Organization, error) {
	org, err := r.isAdminInOrganization(ctx, id)
	if err != nil {
		return nil, e.Wrap(err, "error querying organization")
	}
	return org, nil
}

func (r *queryResolver) Admin(ctx context.Context) (*model.Admin, error) {
	uid := fmt.Sprintf("%v", ctx.Value(model.ContextKeys.UID))
	admin := &model.Admin{UID: &uid}
	res := r.DB.Where(&model.Admin{UID: &uid}).First(&admin)
	if err := res.Error; err != nil {
		firebaseUser, err := AuthClient.GetUser(context.Background(), uid)
		if err != nil {
			return nil, e.Wrap(err, "error retrieving user from firebase api")
		}
		newAdmin := &model.Admin{
			UID:      &uid,
			Name:     &firebaseUser.DisplayName,
			Email:    &firebaseUser.Email,
			PhotoURL: &firebaseUser.PhotoURL,
		}
		if err := r.DB.Create(newAdmin).Error; err != nil {
			return nil, e.Wrap(err, "error creating new admin")
		}
		admin = newAdmin
	}
	if admin.PhotoURL == nil || admin.Name == nil {
		firebaseUser, err := AuthClient.GetUser(context.Background(), uid)
		if err != nil {
			return nil, e.Wrap(err, "error retrieving user from firebase api")
		}
		if err := r.DB.Model(admin).Updates(&model.Admin{
			PhotoURL: &firebaseUser.PhotoURL,
			Name:     &firebaseUser.DisplayName,
		}).Error; err != nil {
			return nil, e.Wrap(err, "error updating org fields")
		}
		admin.PhotoURL = &firebaseUser.PhotoURL
		admin.Name = &firebaseUser.DisplayName
	}
	return admin, nil
}

func (r *queryResolver) Segments(ctx context.Context, organizationID int) ([]*model.Segment, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}
	// list of maps, where each map represents a field query.
	segments := []*model.Segment{}
	if err := r.DB.Where(model.Segment{OrganizationID: organizationID}).Find(&segments).Error; err != nil {
		log.Errorf("error querying segments from organization: %v", err)
	}
	return segments, nil
}

func (r *queryResolver) ErrorSegments(ctx context.Context, organizationID int) ([]*model.ErrorSegment, error) {
	if _, err := r.isAdminInOrganization(ctx, organizationID); err != nil {
		return nil, e.Wrap(err, "admin not found in org")
	}
	// list of maps, where each map represents a field query.
	segments := []*model.ErrorSegment{}
	if err := r.DB.Where(model.ErrorSegment{OrganizationID: organizationID}).Find(&segments).Error; err != nil {
		log.Errorf("error querying segments from organization: %v", err)
	}
	return segments, nil
}

func (r *queryResolver) RecordingSettings(ctx context.Context, organizationID int) (*model.RecordingSettings, error) {
	recordingSettings := &model.RecordingSettings{OrganizationID: organizationID}
	if res := r.DB.Where(&model.RecordingSettings{OrganizationID: organizationID}).First(&recordingSettings); res.Error != nil {
		newRecordSettings := &model.RecordingSettings{
			OrganizationID: organizationID,
			Details:        nil,
		}
		if err := r.DB.Create(newRecordSettings).Error; err != nil {
			return nil, e.Wrap(err, "error creating new recording settings")
		}
		recordingSettings = newRecordSettings
	}
	return recordingSettings, nil
}

func (r *queryResolver) APIKeyToOrgID(ctx context.Context, apiKey string) (*int, error) {
	var orgId int
	if err := r.DB.Table("organizations").Select("id").Where("secret=?", apiKey).Scan(&orgId).Error; err != nil {
		return nil, e.Wrap(err, "error getting org id from api key")
	}
	return &orgId, nil
}

func (r *segmentResolver) Params(ctx context.Context, obj *model.Segment) (*model.SearchParams, error) {
	params := &model.SearchParams{}
	if obj.Params == nil {
		return params, nil
	}
	if err := json.Unmarshal([]byte(*obj.Params), params); err != nil {
		return nil, e.Wrapf(err, "error unmarshaling segment params")
	}
	return params, nil
}

func (r *sessionResolver) UserObject(ctx context.Context, obj *model.Session) (interface{}, error) {
	return obj.UserObject, nil
}

func (r *sessionAlertResolver) ChannelsToNotify(ctx context.Context, obj *model.SessionAlert) ([]*modelInputs.SanitizedSlackChannel, error) {
	return obj.GetChannelsToNotify()
}

func (r *sessionAlertResolver) ExcludedEnvironments(ctx context.Context, obj *model.SessionAlert) ([]*string, error) {
	return obj.GetExcludedEnvironments()
}

func (r *sessionAlertResolver) TrackProperties(ctx context.Context, obj *model.SessionAlert) ([]*model.TrackProperty, error) {
	return obj.GetTrackProperties()
}

func (r *sessionAlertResolver) UserProperties(ctx context.Context, obj *model.SessionAlert) ([]*model.UserProperty, error) {
	return obj.GetUserProperties()
}

func (r *sessionCommentResolver) Author(ctx context.Context, obj *model.SessionComment) (*modelInputs.SanitizedAdmin, error) {
	admin := &model.Admin{}
	if err := r.DB.Where(&model.Admin{Model: model.Model{ID: obj.AdminId}}).First(&admin).Error; err != nil {
		return nil, e.Wrap(err, "Error finding admin for comment")
	}

	name := ""
	email := ""
	photo_url := ""

	if admin.Name != nil {
		name = *admin.Name
	}
	if admin.Email != nil {
		email = *admin.Email
	}
	if admin.PhotoURL != nil {
		photo_url = *admin.PhotoURL
	}

	sanitizedAdmin := &modelInputs.SanitizedAdmin{
		ID:       admin.ID,
		Name:     &name,
		Email:    email,
		PhotoURL: &photo_url,
	}

	return sanitizedAdmin, nil
}

// ErrorAlert returns generated.ErrorAlertResolver implementation.
func (r *Resolver) ErrorAlert() generated.ErrorAlertResolver { return &errorAlertResolver{r} }

// ErrorComment returns generated.ErrorCommentResolver implementation.
func (r *Resolver) ErrorComment() generated.ErrorCommentResolver { return &errorCommentResolver{r} }

// ErrorGroup returns generated.ErrorGroupResolver implementation.
func (r *Resolver) ErrorGroup() generated.ErrorGroupResolver { return &errorGroupResolver{r} }

// ErrorObject returns generated.ErrorObjectResolver implementation.
func (r *Resolver) ErrorObject() generated.ErrorObjectResolver { return &errorObjectResolver{r} }

// ErrorSegment returns generated.ErrorSegmentResolver implementation.
func (r *Resolver) ErrorSegment() generated.ErrorSegmentResolver { return &errorSegmentResolver{r} }

// Mutation returns generated.MutationResolver implementation.
func (r *Resolver) Mutation() generated.MutationResolver { return &mutationResolver{r} }

// Query returns generated.QueryResolver implementation.
func (r *Resolver) Query() generated.QueryResolver { return &queryResolver{r} }

// Segment returns generated.SegmentResolver implementation.
func (r *Resolver) Segment() generated.SegmentResolver { return &segmentResolver{r} }

// Session returns generated.SessionResolver implementation.
func (r *Resolver) Session() generated.SessionResolver { return &sessionResolver{r} }

// SessionAlert returns generated.SessionAlertResolver implementation.
func (r *Resolver) SessionAlert() generated.SessionAlertResolver { return &sessionAlertResolver{r} }

// SessionComment returns generated.SessionCommentResolver implementation.
func (r *Resolver) SessionComment() generated.SessionCommentResolver {
	return &sessionCommentResolver{r}
}

type errorAlertResolver struct{ *Resolver }
type errorCommentResolver struct{ *Resolver }
type errorGroupResolver struct{ *Resolver }
type errorObjectResolver struct{ *Resolver }
type errorSegmentResolver struct{ *Resolver }
type mutationResolver struct{ *Resolver }
type queryResolver struct{ *Resolver }
type segmentResolver struct{ *Resolver }
type sessionResolver struct{ *Resolver }
type sessionAlertResolver struct{ *Resolver }
type sessionCommentResolver struct{ *Resolver }
