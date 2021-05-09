# Waypoint Plugin Lambda Ext(ended)

*Note: still highly experimental and not production ready

Modified from the example repo: [https://github.com/hashicorp/waypoint-plugin-examples](https://github.com/hashicorp/waypoint-plugin-examples)

Adds additional functionality to support:
- EFS Filesystems
- Event Sourcing via EventBridge

Removes automatic creation of:
- Target Groups
- ALB

Example usage:

```hcl
project = "my-project"

app "my-function" {
  build {
    use "docker" {}
    registry {
      use "aws-ecr" {
        region     = "eu-west-1"
        repository = "my-repo/my-function"
        tag        = gitrefpretty()
      }
    }
  }

  deploy {
    use "lambda-ex" {
      region               = "eu-west-1"
      role_arn             = "arn:aws:iam::123456789:role/LambdaExecRole"
      event_source         = "some.custom.event"
      efs_access_point_arn = "arn:aws:elasticfilesystem:XXX"
      efs_mount_path       = "/mnt/efs"
      subnet_ids = [
        "subnet-xxxxxxxx",
        "subnet-xxxxxxxx",
      ]
      security_group_ids = [
        "sg-xxxxxxxx",
      ]
    }
  }
}
```
