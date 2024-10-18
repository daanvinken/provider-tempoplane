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
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/google/uuid"
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
	temporalpb "go.temporal.io/api/enums/v1"
	temporalclient "go.temporal.io/sdk/client"
)

const (
	errNotEntityWorkflow  = "managed resource is not a EntityWorkflow custom resource"
	errTrackPCUsage       = "cannot track ProviderConfig usage"
	errGetPC              = "cannot get ProviderConfig"
	errGetCreds           = "cannot get credentials"
	errTemporalConnection = "cannot connect to temporal"
	errNewKubeClient      = "cannot connect to KubeAPI"
	errUpdateKubeObject   = "cannot update Kubernetes object"
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
			kube:  mgr.GetClient(),
			usage: resource.NewProviderConfigUsageTracker(mgr.GetClient(), &apisv1alpha1.ProviderConfigUsage{}),
		}),
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
	kube     client.Client
	usage    resource.Tracker
	temporal client.Client
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

	tc, err := temporalclient.Dial(temporalclient.Options{
		HostPort:  pc.Spec.Hostname,
		Namespace: pc.Spec.Namespace,
	})

	if err != nil {
		return &external{}, errors.Wrap(err, errTemporalConnection)
	}

	hreq := temporalclient.CheckHealthRequest{}
	_, err = tc.CheckHealth(context.Background(), &hreq)
	if err != nil {
		return &external{}, errors.Wrap(err, errTemporalConnection)
	}

	return &external{
		temporalClient: tc,
		kubeClient:     c.kube,
	}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	temporalClient temporalclient.Client
	kubeClient     client.Client
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

	// Extract WorkflowID from CorrelationID in EntityInput
	workflowID := cr.Status.WorkflowID
	lastRunID := cr.Status.AtProvider.RunID

	if workflowID == "" || lastRunID == "" {
		return managed.ExternalObservation{ResourceExists: false}, nil
	}

	// Describe the workflow execution using the WorkflowID (CorrelationID)
	resp, err := c.temporalClient.DescribeWorkflowExecution(ctx, workflowID, lastRunID)

	// If we are deleting, we'll just check if the latest run was a delete and succeeded. Observe is always called after delete.

	if err != nil {
		//TODO (daanvi) improve error check
		if string(err.Error()) == "operation GetCurrentExecution encountered not found" || string(err.Error()) == "operation GetWorkflowExecution encountered not found" {
			return managed.ExternalObservation{ResourceExists: false}, nil
		}
		return managed.ExternalObservation{}, errors.Wrap(err, "failed to describe workflow execution")
	}

	// Check the status of the workflow from the response
	workflowStatus := resp.WorkflowExecutionInfo.GetStatus()
	switch workflowStatus {
	case temporalpb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		if resp.WorkflowExecutionInfo.Type.Name == "DeleteWorkflow" {
			fmt.Println("Now we done")
			// Indicate that we're about to delete the instance.
			cr.SetConditions(xpv1.Deleting())
			return managed.ExternalObservation{
				ResourceExists: false,
			}, nil
		}
		cr.SetConditions(xpv1.Available())
		// If the workflow has completed successfully
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
			ConnectionDetails: managed.ConnectionDetails{
				"WorkflowID": []byte(workflowID),
				"runID":      []byte(resp.WorkflowExecutionInfo.FirstRunId),
				"Status":     []byte("completed"),
			},
		}, nil
	case temporalpb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		// If the workflow is still running
		return managed.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: false,
		}, nil
	default:
		// Handle other statuses (e.g., FAILED, TIMED_OUT)
		return managed.ExternalObservation{
			ResourceExists:   false,
			ResourceUpToDate: false,
			ConnectionDetails: managed.ConnectionDetails{
				"WorkflowID": []byte(workflowID),
				"runID":      []byte(resp.WorkflowExecutionInfo.Execution.RunId), //TODO correct RunID?
				"Status":     []byte(workflowStatus.String()),
			},
		}, nil
	}
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	mg.SetConditions(xpv1.Creating())
	cr, ok := mg.(*v1alpha1.EntityWorkflow)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotEntityWorkflow)
	}

	// Parse EntityInput from the managed resource's spec
	entityInput := cr.Spec.ForProvider.EntityInput

	// Define the task queue that the workflow worker is listening on
	taskQueue := "TempoPlane-" + entityInput.Kind

	uniqueID := uuid.New().String()
	ID := "TempoPlane" + "-" + uniqueID + "-" + entityInput.RequesterID

	cr.Spec.ForProvider.EntityInput.CorrelationID = ID

	// Configure workflow execution options
	workflowOptions := temporalclient.StartWorkflowOptions{
		TaskQueue: taskQueue,
		ID:        ID,
	}

	cr.Status.WorkflowID = ID

	if err := c.kubeClient.Status().Update(ctx, cr); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errUpdateKubeObject)
	}

	// Execute the CreateWorkflow
	workflowExecution, err := c.temporalClient.ExecuteWorkflow(ctx, workflowOptions, entityworkflow.CreateWorkflow, ConvertToEntityWorkflowInput(entityInput))
	if err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, "failed to start CreateWorkflow")
	}

	cr.Status.AtProvider.RunID = workflowExecution.GetRunID()

	if err := c.kubeClient.Status().Update(ctx, cr); err != nil {
		return managed.ExternalCreation{}, errors.Wrap(err, errUpdateKubeObject)
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

	//TODO (daanvi) Multiple calls seem to happen before delete

	// Parse EntityInput from the managed resource's spec
	entityInput := cr.Spec.ForProvider.EntityInput

	// Define the task queue that the workflow worker is listening on
	taskQueue := "TempoPlane-" + entityInput.Kind

	// Configure workflow execution options
	workflowOptions := temporalclient.StartWorkflowOptions{
		TaskQueue: taskQueue,
		ID:        cr.Status.WorkflowID,
	}

	// Execute the DeleteWorkflow
	workflowExecution, err := c.temporalClient.ExecuteWorkflow(ctx, workflowOptions, entityworkflow.DeleteWorkflow, ConvertToEntityWorkflowInput(entityInput))
	if err != nil {
		return errors.Wrap(err, "failed to start DeleteWorkflow")
	}

	cr.Status.AtProvider.RunID = workflowExecution.GetRunID()
	if err := c.kubeClient.Status().Update(ctx, cr); err != nil {
		return errors.Wrap(err, errUpdateKubeObject)
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

	//TODO (daanvi) Implement deletion workflowID in state, prior to trigger. This way we can fetch in here the intial workflow that triggered deletion.
	// If deletion is failing or already happened, we do not have to retry I think

	// Note that we use resource.Ignore to squash any error that indicates the
	// external resource does not exist. Delete implementations must not return
	// an error when asked to delete a non-existent external resource.
	//return errors.Wrap(resource.Ignore(database.IsNotFound, err), "cannot delete instance")
	return nil
}
