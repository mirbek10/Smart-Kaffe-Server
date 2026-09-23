// init-local configures only the loopback MongoDB development replica set.
package main

import (
	"context"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"log"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client, e := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:27017/?directConnection=true").SetServerSelectionTimeout(5 * time.Second))
	if e != nil {
		log.Fatal("local Mongo configuration failed")
	}
	defer client.Disconnect(ctx)
	db := client.Database("admin")
	var status bson.M
	e = db.RunCommand(ctx, bson.D{{Key: "replSetGetStatus", Value: 1}}).Decode(&status)
	if e != nil {
		var result bson.M
		e = db.RunCommand(ctx, bson.D{{Key: "replSetInitiate", Value: bson.M{"_id": "rs0", "members": []bson.M{{"_id": 0, "host": "127.0.0.1:27017"}}}}}).Decode(&result)
		if e != nil {
			log.Fatal("could not initialize local replica set")
		}
	}
	for ctx.Err() == nil {
		var hello struct {
			Primary bool `bson:"isWritablePrimary"`
		}
		if db.RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello) == nil && hello.Primary {
			log.Print("Local replica set ready")
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Fatal("local replica set did not become primary")
}
