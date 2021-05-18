package release

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/eventbridge"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/waypoint-plugin-sdk/component"
	"github.com/hashicorp/waypoint-plugin-sdk/terminal"
	"github.com/hashicorp/waypoint/builtin/aws/utils"
	"github.com/phoban01/lambda-ext/platform"
)

type ReleaseConfig struct {
	Region      string  `hcl:"region,optional"`
	EventBus    *string `hcl:"event_bus,optional"`
	EventSource *string `hcl:"event_source,optional"`
	Url         string  `hcl:"url,optional"`
}

type ReleaseManager struct {
	config ReleaseConfig
}

// Implement Configurable
func (rm *ReleaseManager) Config() (interface{}, error) {
	return &rm.config, nil
}

// Implement ConfigurableNotify
func (rm *ReleaseManager) ConfigSet(config interface{}) error {
	_, ok := config.(*ReleaseConfig)
	if !ok {
		// The Waypoint SDK should ensure this never gets hit
		return fmt.Errorf("Expected *ReleaseConfig as parameter")
	}

	return nil
}

// Implement Releaser
func (rm *ReleaseManager) ReleaseFunc() interface{} {
	// return a function which will be called by Waypoint
	return rm.release
}

func (rm *ReleaseManager) release(
	ctx context.Context,
	log hclog.Logger,
	ui terminal.UI,
	src *component.Source,
	deploy *platform.Deployment,
) (*Release, error) {

	sg := ui.StepGroup()
	defer sg.Wait()

	step := sg.Add("Connecting to AWS")
	// We put this in a function because if/when step is reassigned, we want to
	// abort the new value.
	defer func() {
		step.Abort()
	}()

	release := &Release{}

	if rm.config.EventBus == nil {
		rm.config.EventBus = aws.String("default")
	}

	sess, err := utils.GetSession(&utils.SessionConfig{
		Region: rm.config.Region,
		Logger: log,
	})
	if err != nil {
		return nil, err
	}

	step.Done()

	//TODO: add flag for create_rule.. by default assume the role already exists
	//and if so don't create or delete the rule
	step = sg.Add("Creating EventBridge Rule")

	evSvc := eventbridge.New(sess)

	rule, err := evSvc.PutRule(&eventbridge.PutRuleInput{
		Name:         aws.String(src.App),
		EventBusName: rm.config.EventBus,
		EventPattern: aws.String(fmt.Sprintf("{\"source\": [\"%s\"]}", *rm.config.EventSource)),
		State:        aws.String("ENABLED"),
	})

	if err != nil {
		return nil, err
	}

	step.Update("Created EventBridge rule: %s", *rule.RuleArn)

	step.Done()

	step = sg.Add("Updating Lambda function version permissions")

	lamSvc := lambda.New(sess)

	_, err = lamSvc.AddPermission(&lambda.AddPermissionInput{
		StatementId:  aws.String(fmt.Sprintf("lambda-eventbridge-%s", src.App)),
		FunctionName: aws.String(deploy.VerArn),
		Action:       aws.String("lambda:InvokeFunction"),
		Principal:    aws.String("events.amazonaws.com"),
		SourceArn:    rule.RuleArn,
	})

	if err != nil {
		return nil, err
	}

	step.Update("Lambda function version permissions updated")

	step.Done()

	step = sg.Add("Creating EventBridge target for Lambda function version")

	_, err = evSvc.PutTargets(&eventbridge.PutTargetsInput{
		Rule: aws.String(src.App),
		Targets: []*eventbridge.Target{
			{
				Id:        aws.String(src.App),
				Arn:       aws.String(deploy.VerArn),
				InputPath: aws.String("$.detail"),
			},
		},
	})

	if err != nil {
		return nil, err
	}

	step.Update("Created EventBridge rule target for Lambda function version")
	step.Done()

	release.EventSource = *rm.config.EventSource
	release.FunctionArn = deploy.VerArn

	return release, nil
}

func (rm *ReleaseManager) DestroyFunc() interface{} {
	return rm.destroy
}

func (rm *ReleaseManager) destroy(ctx context.Context,
	log hclog.Logger,
	ui terminal.UI,
	src *component.Source,
) error {
	// We'll update the user in real time
	st := ui.Status()
	defer st.Close()

	sess, err := utils.GetSession(&utils.SessionConfig{
		Region: rm.config.Region,
		Logger: log,
	})
	if err != nil {
		return err
	}

	evSvc := eventbridge.New(sess)
	_, err = evSvc.RemoveTargets(&eventbridge.RemoveTargetsInput{
		Rule: aws.String(src.App),
		Ids: []*string{
			aws.String(src.App),
		},
	})

	if err != nil {
		return err
	}

	st.Step(terminal.StatusOK, "Deleted EventBridge Target")

	st.Update("Deleting EventBridge rule")
	_, err = evSvc.DeleteRule(&eventbridge.DeleteRuleInput{
		Name: aws.String(src.App),
	})

	if err != nil {
		return err
	}

	st.Step(terminal.StatusOK, "Deleted EventBridge rule")

	return err
}

// ensure Releaser implements component.Release
func (r *Release) URL() string {
	return r.Url
}
