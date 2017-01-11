# ECS Unique Port

Lambda-backed custom CloudFormation resource that produces a unique port.

## How it works

This repo contains:

- A JS wrapper (lambada.js) which handles lambda's api and passes event onto a compile go binary

- Go code to manage a Port free list

The port free list is managed with two DynamoDB tables:

- The first table is used as a distributed lock, since this lambda function will often be called in parallel

- The second table manages a free list of ports in the range 10000 - 50000 as a bitset.  A set bit indicates that that port is available, and an unset bit means that the port has already been allocated for someone else.

## Building and updating

You can build the code with

``` bash
$ make install_deps
$ make
```

This will create the zip file (uniqueport.zip) ready to be uploaded to lambda.  Navigate to your existing lambda function in the aws console and use the upload zip function to upload the newly built zip file.

## Launching from scratch

If you don't already have this lambda function setup, the instructions below will set up the lambda function and all the necessary dynamodb tables.

1. Build the code for the lambda function and upload it to S3:

    ```
export PUBLIC_AWS_BUCKET=clever-public-us-west-2
make release
    ```

    This will upload a file called `uniqueport.zip` into the bucket you specify.

2. Launch the lambda function into a region that has lambda support, e.g. us-west-2:

    ```
aws --region us-west-2 cloudformation create-stack \
  --stack-name custom-cf-resource-uniqueport \
  --template-body file://`pwd`/custom-cf-resource-uniqueport.json \
  --capabilities CAPABILITY_IAM \
  --parameters \
  ParameterKey=S3Bucket,ParameterValue=$PUBLIC_AWS_BUCKET \
  ParameterKey=S3Key,ParameterValue=uniqueport.zip
    ```

3. Now you can use the resource in cloudformation configs.
The following example just asks for a few ports (and does it in a different region than the lamda fn):

    ```
# all of the following are outputs of step 2
export LAMBDA_ARN=x
export DYNAMO_REGION=x
export DYNAMO_LOCK_TABLE=x
export DYNAMO_PORTS_TABLE=x

aws --region us-west-1 cloudformation create-stack \
  --stack-name uniqueport-example \
  --template-body file://`pwd`/example.json \
  --capabilities CAPABILITY_IAM \
  --parameters \
  ParameterKey=Key,ParameterValue=example-key \
  ParameterKey=LambdaArn,ParameterValue=$LAMBDA_ARN \
  ParameterKey=DynamoRegion,ParameterValue=$DYNAMO_REGION \
  ParameterKey=LockTable,ParameterValue=$DYNAMO_LOCK_TABLE \
  ParameterKey=PortsTable,ParameterValue=$DYNAMO_PORTS_TABLE
    ```
