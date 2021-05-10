package platform

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/waypoint-plugin-sdk/terminal"
	"github.com/hashicorp/waypoint/builtin/aws/ecr"
	"github.com/pkg/errors"

	"github.com/hashicorp/waypoint-plugin-sdk/component"
	"github.com/hashicorp/waypoint/builtin/aws/utils"
)

// Deploy deploys an image to AWS Lambda, modified from builtin lambda plugin
// but removing target group arn and adding support for
// efs and eventbridge

type DeployConfig struct {
	Region            string    `hcl:"region,optional"`
	RoleArn           string    `hcl:"role_arn,optional"`
	Memory            int64     `hcl:"memory,optional"`
	Timeout           int64     `hcl:"timeout,optional"`
	SubnetIds         []*string `hcl:"subnet_ids,optional"`
	SecurityGroupIds  []*string `hcl:"security_group_ids,optional"`
	EfsAccessPointArn *string   `hcl:"efs_access_point_arn,optional"`
	EfsMountPath      *string   `hcl:"efs_mount_path,optional"`
}

type Platform struct {
	config DeployConfig
}

// Implement Configurable
func (p *Platform) Config() (interface{}, error) {
	return &p.config, nil
}

// Implement ConfigurableNotify
func (p *Platform) ConfigSet(config interface{}) error {
	c, ok := config.(*DeployConfig)
	if !ok {
		// The Waypoint SDK should ensure this never gets hit
		return fmt.Errorf("Expected *DeployConfig as parameter")
	}

	// validate the config
	if c.Region == "" {
		c.Region = "eu-west-1"
	}

	return nil
}

// Implement Builder
func (p *Platform) DeployFunc() interface{} {
	// return a function which will be called by Waypoint
	return p.deploy
}

const (
	// The default amount of memory to give to the function invocation, 256MB
	DefaultMemory = 256

	// How long the function should run before terminating it.
	DefaultTimeout = 60

	//
	DefaultEnv = "dev"
)

func (p *Platform) deploy(
	ctx context.Context,
	log hclog.Logger,
	src *component.Source,
	job *component.JobInfo,
	img *ecr.Image,
	deployConfig *component.DeploymentConfig,
	ui terminal.UI,
) (*Deployment, error) {

	sg := ui.StepGroup()
	defer sg.Wait()

	step := sg.Add("Connecting to AWS")

	// We put this in a function because if/when step is reassigned, we want to
	// abort the new value.
	defer func() {
		step.Abort()
	}()

	// Start building our deployment since we use this information
	deployment := &Deployment{}
	id, err := component.Id()
	if err != nil {
		return nil, err
	}

	sess, err := utils.GetSession(&utils.SessionConfig{
		Region: p.config.Region,
	})
	if err != nil {
		return nil, err
	}

	roleArn := p.config.RoleArn

	mem := int64(p.config.Memory)
	if mem == 0 {
		mem = DefaultMemory
	}

	timeout := int64(p.config.Timeout)
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	env := job.Workspace
	if env == "default" {
		env = DefaultEnv
	}

	step.Done()

	step = sg.Add("Reading Lambda function: %s", src.App)

	lamSvc := lambda.New(sess)
	curFunc, err := lamSvc.GetFunction(&lambda.GetFunctionInput{
		FunctionName: aws.String(src.App),
	})

	var funcarn string

	// If the function exists (ie we read it), we update it's code rather than create a new one.
	if err == nil {
		step.Update("Updating Lambda function with new code")

		var reset bool
		var update lambda.UpdateFunctionConfigurationInput

		if *curFunc.Configuration.MemorySize != mem {
			update.MemorySize = aws.Int64(mem)
			reset = true
		}

		if *curFunc.Configuration.Timeout != timeout {
			update.Timeout = aws.Int64(timeout)
			reset = true
		}

		update.VpcConfig = &lambda.VpcConfig{
			SubnetIds:        p.config.SubnetIds,
			SecurityGroupIds: p.config.SecurityGroupIds,
		}

		reset = true

		if reset {
			update.FunctionName = curFunc.Configuration.FunctionArn

			_, err = lamSvc.UpdateFunctionConfiguration(&update)
			if err != nil {
				return nil, errors.Wrapf(err, "unable to update function configuration")
			}
		}

		funcCfg, err := lamSvc.UpdateFunctionCode(&lambda.UpdateFunctionCodeInput{
			FunctionName: aws.String(src.App),
			ImageUri:     aws.String(img.Name()),
		})

		if err != nil {
			return nil, err
		}

		funcarn = *funcCfg.FunctionArn

		if err != nil {
			return nil, err
		}

		// We couldn't read the function before, so we'll go ahead and create one.
	} else {
		step.Update("Creating new Lambda function")

		// Run this in a loop to guard against eventual consistency errors with the specified
		// role not showing up within lambda right away.
		for i := 0; i < 30; i++ {
			funcOut, err := lamSvc.CreateFunction(&lambda.CreateFunctionInput{
				Description:  aws.String(fmt.Sprintf("waypoint %s", src.App)),
				FunctionName: aws.String(src.App),
				Role:         aws.String(roleArn),
				Timeout:      aws.Int64(timeout),
				MemorySize:   aws.Int64(mem),
				Tags: map[string]*string{
					"waypoint.app": aws.String(src.App),
				},
				PackageType: aws.String("Image"),
				Code: &lambda.FunctionCode{
					ImageUri: aws.String(img.Name()),
				},
				ImageConfig: &lambda.ImageConfig{},
				VpcConfig: &lambda.VpcConfig{
					SubnetIds:        p.config.SubnetIds,
					SecurityGroupIds: p.config.SecurityGroupIds,
				},
				FileSystemConfigs: []*lambda.FileSystemConfig{
					{
						Arn:            p.config.EfsAccessPointArn,
						LocalMountPath: p.config.EfsMountPath,
					},
				},
				Environment: &lambda.Environment{
					Variables: map[string]*string{
						"ENV": aws.String(env),
					},
				},
			})

			if err != nil {
				// if we encounter an unrecoverable error, exit now.
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					case "ResourceConflictException":
						return nil, err
					}
				}

				// otherwise sleep and try again
				time.Sleep(2 * time.Second)
			} else {
				funcarn = *funcOut.FunctionArn
				break
			}
		}
	}

	if funcarn == "" {
		return nil, fmt.Errorf("Unable to create function, timed out trying")
	}

	step.Done()

	step = sg.Add("Waiting for Lambda function to be processed")
	// The image is never ready right away, AWS has to process it, so we wait
	// 3 seconds before trying to publish the version

	time.Sleep(3 * time.Second)

	// no publish this new code to create a stable identifier for it. Otherwise
	// if a manually pushes to the function and we use $LATEST, we'll accidentally
	// start running their manual code rather then the fixed one we have here.
	var ver *lambda.FunctionConfiguration

	// Only try 30 times.
	for i := 0; i < 30; i++ {
		ver, err = lamSvc.PublishVersion(&lambda.PublishVersionInput{
			FunctionName: aws.String(src.App),
		})

		if err == nil {
			break
		}

		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case "ResourceConflictException":
				// It's updating, wait a sec and try again
				time.Sleep(time.Second)
			default:
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	if ver == nil {
		return nil, fmt.Errorf("Lambda was unable to prepare the function in the aloted time")
	}

	verarn := *ver.FunctionArn

	step.Update("Published Lambda function: %s (%s)", verarn, *ver.Version)
	step.Done()

	deployment.Region = p.config.Region
	deployment.Id = id
	deployment.FuncArn = funcarn
	deployment.VerArn = verarn
	deployment.Version = *ver.Version

	return deployment, nil
}

func (p *Platform) DestroyFunc() interface{} {
	return p.destroy
}

func (p *Platform) destroy(ctx context.Context,
	log hclog.Logger,
	deployment *Deployment,
	ui terminal.UI,
	src *component.Source,
) error {
	// We'll update the user in real time
	st := ui.Status()
	defer st.Close()

	sess, err := utils.GetSession(&utils.SessionConfig{
		Region: p.config.Region,
		Logger: log,
	})
	if err != nil {
		return err
	}

	st.Update("Deleting Lambda function version " + deployment.Version)

	lamSvc := lambda.New(sess)

	if deployment.Version != "" {
		_, err = lamSvc.DeleteFunction(&lambda.DeleteFunctionInput{
			FunctionName: aws.String(deployment.FuncArn),
			Qualifier:    aws.String(deployment.Version),
		})
		if err != nil {
			return err
		}
	}
	st.Step(terminal.StatusOK, "Deleted Lambda function version")

	return err
}

func (p *Platform) DestroyWorkspaceFunc() interface{} {
	return p.destroyWorkspace
}

func (p *Platform) destroyWorkspace(ctx context.Context,
	log hclog.Logger,
	ui terminal.UI,
	src *component.Source,
) error {
	// We'll update the user in real time
	st := ui.Status()
	defer st.Close()

	sess, err := utils.GetSession(&utils.SessionConfig{
		Region: p.config.Region,
		Logger: log,
	})
	if err != nil {
		return err
	}

	st.Update("Deleting Lambda function")

	lamSvc := lambda.New(sess)

	_, err = lamSvc.DeleteFunction(&lambda.DeleteFunctionInput{
		FunctionName: aws.String(src.App),
	})

	if err != nil {
		return err
	}

	st.Step(terminal.StatusOK, "Deleted Lambda function")

	return nil
}
