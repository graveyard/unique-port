package dynamodb

import (
	"fmt"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
)

func PopRandom() {
	initialSetMembers := map[string]struct{}{
		"10000": struct{}{},
		"10001": struct{}{},
		"10002": struct{}{},
	}
	region := os.Getenv("TEST_DYNAMODB_REGION")
	endpoint := os.Getenv("TEST_DYNAMODB_ENDPOINT")
	lockTable := os.Getenv("TEST_DYNAMODB_LOCKTABLE")
	portsTable := os.Getenv("TEST_DYNAMODB_PORTSTABLE")
	if region == "" || endpoint == "" || lockTable == "" || portsTable == "" {
		fmt.Printf("must specify TEST_DYNAMODB_REGION, TEST_DYNAMODB_ENDPOINT, TEST_DYNAMODB_LOCKTABLE and TEST_DYNAMODB_PORTSTABLE")
		return
	}
	config := &aws.Config{
		Region:   aws.String(region),
		Endpoint: aws.String(endpoint),
	}
	d := New(config, lockTable, portsTable, fmt.Sprintf("test_%d", time.Now().Unix()), time.Second*30)
	popped := map[string]struct{}{}
	poppedMu := sync.Mutex{}
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			el, err := d.PopRandom()
			if err != nil {
				fmt.Printf("unexpected PopRandom error: %s", err)
			}
			poppedMu.Lock()
			popped[el] = struct{}{}
			poppedMu.Unlock()
		}()
	}
	wg.Wait()

	if reflect.DeepEqual(initialSetMembers, popped) {
		fmt.Println("popped all initial members")
	}
	// Output:
	// popped all initial members
}
