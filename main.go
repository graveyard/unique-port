package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Clever/unique-port/dynamodb"
	"github.com/aws/aws-sdk-go/aws"
)

// Message is the message published by CloudFormation into the SNS topic.
type Message struct {
	Records []Record
}

type Record struct {
	EventSource          string
	EventVersion         string
	EventSubscriptionArn string
	Sns                  SNS
}

type SNS struct {
	MessageId        string
	ReceiptHandle    string
	Type             string
	TopicArn         string
	Subject          string
	Message          string
	Timestamp        time.Time
	SignatureVersion string
	Signature        string
	SigningCertURL   string
	UnsubscribeURL   string
}

// CFRequest is the CloudFormation request.
type CFRequest struct {
	ResourceType string
	RequestType  string

	RequestId          string
	StackId            string
	LogicalResourceId  string
	PhysicalResourceId string
	ResponseURL        string

	ResourceProperties map[string]interface{}
}

// CFResponse is what needs to be returned to CloudFormation.
type CFResponse struct {
	RequestId         string
	StackId           string
	LogicalResourceId string

	Data               map[string]string
	PhysicalResourceId string
	Reason             string
	Status             string
}

// HandleFormation handles the message sent by CF containing what it wants.
func HandleFormation(sns SNS) error {
	var cfreq CFRequest

	if err := json.Unmarshal([]byte(sns.Message), &cfreq); err != nil {
		return err
	}

	if err := HandleRequest(cfreq); err != nil {
		return err
	}

	return nil
}

func HandleRequest(cfreq CFRequest) error {
	log.Printf("got request %#v", cfreq)

	var err error
	var outputs map[string]string
	physical := cfreq.PhysicalResourceId

	if cfreq.ResourceType != "Custom::UniquePort" {
		if cfreq.RequestType == "Delete" {
			log.Printf("treating delete of unknown resource type %s as a no-op", cfreq.ResourceType)
		} else {
			err = fmt.Errorf("unsupported resource type: %s", cfreq.ResourceType)
		}
	} else {
		physical, outputs, err = HandleUniquePort(cfreq)
	}

	cfres := CFResponse{
		RequestId:          cfreq.RequestId,
		StackId:            cfreq.StackId,
		LogicalResourceId:  cfreq.LogicalResourceId,
		PhysicalResourceId: physical,
		Status:             "SUCCESS",
		Data:               outputs,
	}

	if err != nil {
		log.Printf("error: %s\n", err)
		cfres.Reason = err.Error()
		cfres.Status = "FAILED"
	}

	if err := putResponse(cfreq.ResponseURL, cfres); err != nil {
		return err
	}

	return nil
}

type UniquePortProperties struct {
	DynamoRegion      string
	DynamoEndpoint    string
	DynamoLockTable   string
	DynamoTable       string
	Key               string
	InitialPortRanges []string
}

func HandleUniquePort(cfreq CFRequest) (string, map[string]string, error) {
	// validate & parse
	var props UniquePortProperties
	if bs, err := json.Marshal(cfreq.ResourceProperties); err != nil {
		return cfreq.PhysicalResourceId, nil, err
	} else {
		if err := json.Unmarshal(bs, &props); err != nil {
			return cfreq.PhysicalResourceId, nil, err
		}
	}
	if props.DynamoRegion == "" {
		return cfreq.PhysicalResourceId, nil, fmt.Errorf("Most provide 'DynamoRegion'")
	} else if props.DynamoEndpoint == "" {
		return cfreq.PhysicalResourceId, nil, fmt.Errorf("Most provide 'DynamoEndpoint'")
	} else if props.DynamoLockTable == "" {
		return cfreq.PhysicalResourceId, nil, fmt.Errorf("Most provide 'DynamoLockTable'")
	} else if props.DynamoTable == "" {
		return cfreq.PhysicalResourceId, nil, fmt.Errorf("Most provide 'DynamoTable'")
	} else if props.Key == "" {
		return cfreq.PhysicalResourceId, nil, fmt.Errorf("Most provide 'Key'")
	}

	config := &aws.Config{
		Region:   aws.String(props.DynamoRegion),
		Endpoint: aws.String(props.DynamoEndpoint),
	}
	d := dynamodb.New(config, props.DynamoLockTable, props.DynamoTable, props.Key, 30*time.Second)

	switch cfreq.RequestType {
	case "Create":
		log.Print("GETTING UNIQUE PORT")
		port, err := d.PopRandom()
		if err != nil {
			return cfreq.PhysicalResourceId, nil, fmt.Errorf("dynamodb error: %s", err)
		}
		if _, err := strconv.Atoi(port); err != nil {
			return cfreq.PhysicalResourceId, nil, fmt.Errorf("non-integer port popped from set: %s", port)
		}
		return props.Key + "-" + port, map[string]string{"Port": port}, nil
	case "Update":
		log.Printf("UPDATING UNIQUE PORT")
		return cfreq.PhysicalResourceId, nil, fmt.Errorf("cannot update")
	case "Delete":
		log.Printf("DELETING UNIQUE PORT")
		parts := strings.Split(cfreq.PhysicalResourceId, "-")
		port := parts[len(parts)-1]
		log.Printf("parsed port=%s from resource id=%s", port, cfreq.PhysicalResourceId)
		if _, err := strconv.Atoi(port); err != nil {
			return cfreq.PhysicalResourceId, nil, nil
		}
		if err := d.Add(port); err != nil {
			return cfreq.PhysicalResourceId, nil, fmt.Errorf("dynamodb error: %s", err)
		}
		return cfreq.PhysicalResourceId, nil, nil
	}

	return "", nil, fmt.Errorf("unknown RequestType: %s", cfreq.RequestType)
}

func putResponse(rurl string, cfres CFResponse) error {
	log.Printf("responding with %#v", cfres)
	data, err := json.Marshal(cfres)
	if err != nil {
		return err
	}

	req, _ := http.NewRequest("PUT", "", bytes.NewBuffer(data))

	parts := strings.SplitN(rurl, "/", 4)
	if len(parts) != 4 {
		return fmt.Errorf("unexpected response url: %s", rurl)
	}
	req.URL.Scheme = parts[0][0 : len(parts[0])-1]
	req.URL.Host = parts[2]
	req.URL.Opaque = fmt.Sprintf("//%s/%s", parts[2], parts[3])

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	rr, _ := ioutil.ReadAll(res.Body)
	log.Printf("string(rr) %+v\n", string(rr))

	return nil
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("main: must specify event as argument")
	}

	data := []byte(os.Args[1])
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Fatalf("main: %s", err)
	}

	log.Printf("main msg = %+v\n", msg)

	for _, rec := range msg.Records {
		if err := HandleFormation(rec.Sns); err != nil {
			log.Fatalf("main: HandleFormation error %s", err)
		}
	}
}
