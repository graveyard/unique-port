// Package dynamodb provides a dynamoDB-backed distributed set.
package dynamodb

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/dfuentes/ddbsync"
	"github.com/willf/bitset"
)

const (
	PortLowerBound  uint = 10000
	PortRangeLength uint = 50000
)

// DynamoDB -backed distributed set.
type DynamoDB struct {
	key       string
	tableLock sync.Locker
	table     *dynamodb.DynamoDB
	tableName string
	timeout   time.Duration
}

// New creates a new dynamodb-backed set.
// It will be stored as an item in a dynamodb table, and it will have a fixed initial membership.
// We need a lock table in order to control access to this item.
func New(config *aws.Config, lockTable, table, key string, timeout time.Duration) *DynamoDB {
	dber := ddbsync.NewDatabaseWithConfig(lockTable, config)

	return &DynamoDB{
		key:       key,
		tableLock: ddbsync.NewMutex(fmt.Sprintf("%s-%s", table, key), int64(15*time.Second), dber),
		table:     dynamodb.New(config),
		tableName: table,
		timeout:   timeout,
	}
}

type tableItem struct {
	Key     string
	Members *bitset.BitSet
}

func marshal(p tableItem) (map[string]*dynamodb.AttributeValue, error) {
	compressedMemberList, err := p.Members.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal bitset to byte slice: %s", err)
	}
	return map[string]*dynamodb.AttributeValue{
		"Key": &dynamodb.AttributeValue{
			S: aws.String(p.Key),
		},
		"Members": &dynamodb.AttributeValue{
			B: compressedMemberList,
		},
	}, nil
}

func unmarshal(item map[string]*dynamodb.AttributeValue, p *tableItem) error {
	if key, ok := item["Key"]; !ok {
		return fmt.Errorf("unmarshal: item doesn't have Key")
	} else if key.S == nil {
		return fmt.Errorf("unmarshal: Key isn't a String")
	} else {
		p.Key = *key.S
	}
	if members, ok := item["Members"]; !ok {
		return fmt.Errorf("unmarshal: item doesn't have Members")
	} else if members.B == nil {
		return fmt.Errorf("unmarshal: Members a binary blob")
	} else {
		p.Members = bitset.New(PortRangeLength)
		err := p.Members.UnmarshalBinary(members.B)
		if err != nil {
			return fmt.Errorf("could not unmarshal bitset from dyanamo item: %s", err)
		}
	}
	return nil
}

func (d *DynamoDB) findOrCreateTableItem() (*tableItem, error) {
	out, err := d.table.GetItem(&dynamodb.GetItemInput{
		ConsistentRead: aws.Bool(true),
		Key: map[string]*dynamodb.AttributeValue{
			"Key": &dynamodb.AttributeValue{
				S: aws.String(d.key),
			},
		},
		TableName: aws.String(d.tableName),
	})
	if err != nil {
		return nil, fmt.Errorf("GetItem error: %s", err)
	}

	if len(out.Item) == 0 {
		// doesn't exist, create it
		pti := tableItem{Key: d.key}
		pti.Members = bitset.New(PortRangeLength)
		var i uint
		for i = 0; i < PortRangeLength; i++ {
			pti.Members.Set(i)
		}
		log.Printf("saving initial item %v", pti)
		item, err := marshal(pti)
		if err != nil {
			return nil, err
		}
		if _, err := d.table.PutItem(&dynamodb.PutItemInput{
			Item:      item,
			TableName: aws.String(d.tableName),
		}); err != nil {
			return nil, err
		}
		log.Print("saved initial item")
		return d.findOrCreateTableItem()
	}

	var pti tableItem
	if err := unmarshal(out.Item, &pti); err != nil {
		return nil, err
	}
	log.Printf("got item '%s' with %d ports", pti.Key, pti.Members.Count())
	return &pti, nil
}

// PopRandom remove a random element from the set.
func (d *DynamoDB) PopRandom() (string, error) {
	log.Print("waiting for lock")
	done := make(chan struct{})
	go func() {
		d.tableLock.Lock()
		close(done)
	}()
	select {
	case <-time.After(d.timeout):
		return "", fmt.Errorf("timed out waiting for dynamo lock")
	case <-done:
		log.Print("lock acquired")
	}
	defer d.tableLock.Unlock()

	// find or create item
	pti, err := d.findOrCreateTableItem()
	if err != nil {
		return "", err
	}

	// get next available member
	n, ok := pti.Members.NextSet(0)
	if !ok {
		return "", errors.New("No ports remaining")
	}
	pti.Members.Clear(n)

	// save item
	item, err := marshal(*pti)
	if err != nil {
		return "", err
	}
	if _, err := d.table.PutItem(&dynamodb.PutItemInput{
		Item:      item,
		TableName: aws.String(d.tableName),
	}); err != nil {
		return "", err
	}

	n += PortLowerBound
	log.Printf("got port %d", int(n))
	return strconv.Itoa(int(n)), nil
}

// Add an element to the set.
// If the set hasn't been initialized with the initial member list, it will be initialized first.
func (d *DynamoDB) Add(member string) error {
	log.Print("waiting for lock")
	done := make(chan struct{})
	go func() {
		d.tableLock.Lock()
		close(done)
	}()
	select {
	case <-time.After(d.timeout):
		return fmt.Errorf("timed out waiting for dynamo lock")
	case <-done:
		log.Print("lock acquired")
	}
	defer d.tableLock.Unlock()

	// find or create item
	pti, err := d.findOrCreateTableItem()
	if err != nil {
		return err
	}

	memberInt, err := strconv.Atoi(member)
	if err != nil {
		return errors.New("could not convert port string to int")
	}

	index := uint(memberInt) - PortLowerBound

	// add member to the set
	pti.Members.Set(index)

	// save item
	item, err := marshal(*pti)
	if err != nil {
		return err
	}
	if _, err := d.table.PutItem(&dynamodb.PutItemInput{
		Item:      item,
		TableName: aws.String(d.tableName),
	}); err != nil {
		return err
	}

	return nil
}
