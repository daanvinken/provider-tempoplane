/*
Copyright 2022 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package entityworkflow

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"os"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/connection"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/daanvinken/tempoplane/pkg/entityworkflow"

	"github.com/daanvinken/provider-tempoplane/apis/orchestration/v1alpha1"
	apisv1alpha1 "github.com/daanvinken/provider-tempoplane/apis/v1alpha1"
	"github.com/daanvinken/provider-tempoplane/internal/features"
	temporalclient "go.temporal.io/sdk/client"
)

const (
	errNotEntityWorkflow = "managed resource is not a EntityWorkflow custom resource"
	errTrackPCUsage      = "cannot track ProviderConfig usage"
	errGetPC             = "cannot get ProviderConfig"
	errGetCreds          = "cannot get credentials"

	errNewClient = "cannot create new Service"
)

// A NoOpService does nothing.
type NoOpService struct{}

var (
	newNoOpService = func(_ []byte) (interface{}, error) { return &NoOpService{}, nil }
)

// Setup adds a controller that reconciles EntityWorkflow managed resources.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := managed.ControllerName(v1alpha1.EntityWorkflowGroupKind)

	cps := []managed.ConnectionPublisher{managed.NewAPISecretPublisher(mgr.GetClient(), mgr.GetScheme())}
	if o.Features.Enabled(features.EnableAlphaExternalSecretStores) {
		cps = append(cps, connection.NewDetailsManager(mgr.GetClient(), apisv1alpha1.StoreConfigGroupVersionKind))
	}

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1alpha1.EntityWorkflowGroupVersionKind),
		managed.WithExternalConnecter(&connector{
			kube:         mgr.GetClient(),
			usage:        resource.NewProviderConfigUsageTracker(mgr.GetClient(), &apisv1alpha1.ProviderConfigUsage{}),
			newServiceFn: newNoOpService}),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithPollInterval(o.PollInterval),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithConnectionPublishers(cps...))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		WithEventFilter(resource.DesiredStateChanged()).
		For(&v1alpha1.EntityWorkflow{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}

// A connector is expected to produce an ExternalClient when its Connect method
// is called.
type connector struct {
	kube         client.Client
	usage        resource.Tracker
	newServiceFn func(creds []byte) (interface{}, error)
}

// Connect typically produces an ExternalClient by:
// 1. Tracking that the managed resource is using a ProviderConfig.
// 2. Getting the managed resource's ProviderConfig.
// 3. Getting the credentials specified by the ProviderConfig.
// 4. Using the credentials to form a client.
func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*v1alpha1.EntityWorkflow)
	if !ok {
		return nil, errors.New(errNotEntityWorkflow)
	}

	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	pc := &apisv1alpha1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}

	cd := pc.Spec.Credentials
	data, err := resource.CommonCredentialExtractor(ctx, cd.Source, c.kube, cd.CommonCredentialSelectors)
	if err != nil {
		return nil, errors.Wrap(err, errGetCreds)
	}

	svc, err := c.newServiceFn(data)
	if err != nil {
		return nil, errors.Wrap(err, errNewClient)
	}

	return &external{service: svc}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	// A 'client' used to connect to the external resource API. In practice this
	// would be something like an AWS SDK client.
	service interface{}
}

func ConvertToEntityWorkflowInput(input v1alpha1.EntityInput) entityworkflow.EntityInput {
	return entityworkflow.EntityInput{
		EntityID:      input.EntityID,
		Kind:          input.Kind,
		APIVersion:    input.APIVersion,
		Data:          input.Data,
		RequesterID:   input.RequesterID,
		DC:            input.DC,
		Env:           input.Env,
		Timestamp:     input.Timestamp,
		CorrelationID: input.CorrelationID,
	}
}

func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1alpha1.EntityWorkflow)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotEntityWorkflow)
	}

	// These fmt statements should be removed in the real implementation.
	fmt.Printf("Observing: %+v", cr)

	return managed.ExternalObservation{
		// Return false when the external resource does not exist. This lets
		// the managed resource reconciler know that it needs to call Create to
		// (re)create the resource, or that it has successfully been deleted.
		ResourceExists: false,

		// Return false when the external resource exists, but it not up to date
		// with the desired managed resource state. This lets the managed
		// resource reconciler know that it needs to call Update.
		ResourceUpToDate: true,

		// Return any details that may be required to connect to the external
		// resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1alpha1.EntityWorkflow)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotEntityWorkflow)
	}

	// Parse EntityInput from the managed resource's spec
	entityInput := cr.Spec.ForProvider.EntityInput

	// Define the task queue that the workflow worker is listening on
	taskQueue := "TempoPlane-" + entityInput.Kind

	uniqueID := uuid.New().String()
	//return fmt.Sprintf("%s-%s-%s-%s", operationType, entityID, requesterID, uniqueID)
	ID := "TempoPlane-" + uniqueID + "-" + entityInput.RequesterID

	// Configure workflow execution options
	workflowOptions := temporalclient.StartWorkflowOptions{
		TaskQueue: taskQueue,
		ID:        ID,
	}

	// Create a Temporal client (assumes you have setup to obtain this client externally, adjust as needed)
	tc, err := temporalclient.Dial(temporalclient.Options{
		HostPort:  os.Getenv("TEMPORAL_ADDRESS"),
		Namespace: os.Getenv("TEMPORAL_NS"),
	})
	defer tc.Close()
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "failed to create Temporal client")
	}

	// Execute the CreateWorkflow
	workflowExecution, err := tc.ExecuteWorkflow(ctx, workflowOptions, entityworkflow.CreateWorkflow, ConvertToEntityWorkflowInput(entityInput))
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "failed to start CreateWorkflow")
	}

	// Log the WorkflowID and RunID for reference
	fmt.Printf("Started CreateWorkflow with WorkflowID: %s and RunID: %s\n", workflowExecution.GetID(), workflowExecution.GetRunID())

	// Optional: Wait for the workflow to complete and retrieve the result
	var createResult entityworkflow.EntityOutput
	err = workflowExecution.Get(ctx, &createResult)
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "failed to get CreateWorkflow result")
	}
	fmt.Printf("CreateWorkflow completed successfully with result: %s\n", createResult.Message)

	// Return the ExternalCreation struct as required by Crossplane
	return managed.ExternalCreation{
		// You can add connection details here if required
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1alpha1.EntityWorkflow)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotEntityWorkflow)
	}

	fmt.Printf("Updating: %+v", cr)

	return managed.ExternalUpdate{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*v1alpha1.EntityWorkflow)
	if !ok {
		return errors.New(errNotEntityWorkflow)
	}

	// Parse EntityInput from the managed resource's spec
	entityInput := cr.Spec.ForProvider.EntityInput

	// Define the task queue that the workflow worker is listening on
	taskQueue := "TempoPlane-" + entityInput.Kind

	uniqueID := uuid.New().String()
	ID := "TempoPlane-Delete-" + uniqueID + "-" + entityInput.RequesterID

	// Configure workflow execution options
	workflowOptions := temporalclient.StartWorkflowOptions{
		TaskQueue: taskQueue,
		ID:        ID,
	}

	//TODO DRY this stuff

	// Create a Temporal client
	tc, err := temporalclient.Dial(temporalclient.Options{
		HostPort:  os.Getenv("TEMPORAL_ADDRESS"),
		Namespace: os.Getenv("TEMPORAL_NS"),
	})
	defer tc.Close()
	if err != nil {
		return errors.Wrap(err, "failed to create Temporal client")
	}

	// Execute the DeleteWorkflow
	workflowExecution, err := tc.ExecuteWorkflow(ctx, workflowOptions, entityworkflow.DeleteWorkflow, ConvertToEntityWorkflowInput(entityInput))
	if err != nil {
		return errors.Wrap(err, "failed to start DeleteWorkflow")
	}

	// Log the WorkflowID and RunID for reference
	fmt.Printf("Started DeleteWorkflow with WorkflowID: %s and RunID: %s\n", workflowExecution.GetID(), workflowExecution.GetRunID())

	// Optional: Wait for the workflow to complete and retrieve the result
	var deleteResult entityworkflow.EntityOutput
	err = workflowExecution.Get(ctx, &deleteResult)
	if err != nil {
		return errors.Wrap(err, "failed to get DeleteWorkflow result")
	}
	fmt.Printf("DeleteWorkflow completed successfully with result: %s\n", deleteResult.Message)

	// Return nil as there's no ExternalCreation required for deletions
	return nil
}
