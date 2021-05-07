package pagerduty

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

var cacheEnabled bool = false
var cacheMongoURL string = ""
var cacheMaxAge, _ = time.ParseDuration("10s")

var mongoClient *mongo.Client
var usersCollection *mongo.Collection
var contactMethodsCollection *mongo.Collection
var miscCollection *mongo.Collection

type CacheAbilitiesRecord struct {
	Endpoint  string
	Abilities *ListAbilitiesResponse
}

type CacheLastRefreshRecord struct {
	Endpoint  string
	Users     time.Time
	Abilities time.Time
}

func InitCache() {
	if cacheMongoURL = os.Getenv("TF_PAGERDUTY_CACHE"); cacheMongoURL == "" {
		log.Println("===== PagerDuty Cache Skipping Init =====")
		return
	}

	if os.Getenv("TF_PAGERDUTY_CACHE_MAX_AGE") != "" {
		d, err := time.ParseDuration(os.Getenv("TF_PAGERDUTY_CACHE_MAX_AGE"))
		if err != nil {
			log.Printf("===== PagerDuty Cache couldn't parse max age %q, using the default =====", os.Getenv("TF_PAGERDUTY_CACHE_MAX_AGE"))
		} else {
			cacheMaxAge = d
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mongoClient, _ = mongo.Connect(ctx, options.Client().ApplyURI(cacheMongoURL))

	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := mongoClient.Ping(ctx, readpref.Primary())
	if err == nil {
		cacheEnabled = true
		log.Println("===== Enabling PagerDuty Cache =====")
	} else {
		cacheEnabled = false
		log.Printf("===== PagerDuty Cache couldn't connect to MongoDB at %q =====", cacheMongoURL)
	}

	usersCollection = mongoClient.Database("pagerduty").Collection("users")
	contactMethodsCollection = mongoClient.Database("pagerduty").Collection("contact_methods")
	miscCollection = mongoClient.Database("pagerduty").Collection("misc")
}

func PopulateCache(c *Client) {
	if !cacheEnabled {
		return
	}
	filter := bson.D{primitive.E{Key: "endpoint", Value: "lastrefresh"}}
	lastRefreshRecord := new(CacheLastRefreshRecord)
	err := miscCollection.FindOne(context.TODO(), filter).Decode(lastRefreshRecord)
	if err == nil {
		if time.Since(lastRefreshRecord.Users) < cacheMaxAge {
			log.Printf("===== PagerDuty cache was refreshed at %s, not refreshing =====", lastRefreshRecord.Users.Format(time.RFC3339))
			return
		} else {
			log.Printf("===== PagerDuty cache was refreshed at %s, refreshing =====", lastRefreshRecord.Users.Format(time.RFC3339))
		}
	}

	usersCollection.Drop(context.TODO())
	contactMethodsCollection.Drop(context.TODO())
	miscCollection.Drop(context.TODO())

	var pdo = ListUsersOptions{
		Include: []string{"contact_methods", "teams"},
	}

	fullUsers, err := c.Users.ListAll(&pdo)
	if err != nil {
		log.Println("===== Couldn't load users =====")
		return
	}

	users := make([]interface{}, len(fullUsers))
	var contactMethods []interface{}
	for i := 0; i < len(fullUsers); i++ {
		user := new(User)
		b, _ := json.Marshal(fullUsers[i])
		json.Unmarshal(b, user)
		users[i] = &user

		for j := 0; j < len(fullUsers[i].ContactMethods); j++ {
			contactMethods = append(contactMethods, &(fullUsers[i].ContactMethods[j]))
		}
	}

	// ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// defer cancel()
	res, err := usersCollection.InsertMany(context.TODO(), users)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Inserted %d users", len(res.InsertedIDs))

	res, err = contactMethodsCollection.InsertMany(context.TODO(), contactMethods)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Inserted %d contact methods", len(res.InsertedIDs))

	abilities, _, _ := c.Abilities.List()

	abilitiesRecord := &CacheAbilitiesRecord{
		Endpoint:  "abilities",
		Abilities: abilities,
	}
	ares, err := miscCollection.InsertOne(context.TODO(), &abilitiesRecord)
	log.Println(ares)
	if err != nil {
		log.Fatal(err)
	}

	cacheLastRefreshRecord := &CacheLastRefreshRecord{
		Endpoint:  "lastrefresh",
		Users:     time.Now(),
		Abilities: time.Now(),
	}
	cres, err := miscCollection.InsertOne(context.TODO(), &cacheLastRefreshRecord)
	log.Println(cres)
	if err != nil {
		log.Fatal(err)
	}
}

func cacheGetAbilities(v interface{}) error {
	log.Println("===== Get abilities from cache =====")
	if !cacheEnabled {
		return &Error{Message: "Cache is not enabled"}
	}
	filter := bson.D{primitive.E{Key: "endpoint", Value: "abilities"}}
	a := new(CacheAbilitiesRecord)
	err := miscCollection.FindOne(context.TODO(), filter).Decode(a)
	if err != nil {
		log.Println(err)
		return err
	}
	b, _ := json.Marshal(a.Abilities)
	_ = json.Unmarshal(b, v)
	log.Println("===== Got abilities from cache =====")
	return nil
}

func cacheInsertUser(u *User) error {
	if !cacheEnabled {
		return &Error{Message: "Cache is not enabled"}
	}

	res, err := usersCollection.InsertOne(context.TODO(), &u)
	if err != nil {
		log.Printf("===== cacheInsertUser error: %q", err)
		return err
	}
	log.Printf("===== cacheInsertUser %+v", res)
	return nil
}

func cacheGetUser(id string, v interface{}) error {
	if !cacheEnabled {
		return &Error{Message: "Cache is not enabled"}
	}
	filter := bson.D{primitive.E{Key: "id", Value: id}}
	r := usersCollection.FindOne(context.TODO(), filter)
	err := r.Decode(v)

	if err != nil {
		log.Println(err)
		return err
	}
	log.Printf("===== Got user %q from cache =====", id)
	return nil
}

func cacheUpdateUser(u *User) error {
	if !cacheEnabled {
		return &Error{Message: "Cache is not enabled"}
	}

	filter := bson.D{primitive.E{Key: "id", Value: u.ID}}
	opts := options.Replace().SetUpsert(true)
	res, err := usersCollection.ReplaceOne(context.TODO(), filter, &u, opts)
	if err != nil {
		log.Printf("===== Error updating user: %q", err)
		return err
	}
	if res.MatchedCount != 0 {
		log.Println("===== replaced an existing user")
		return nil
	}
	if res.UpsertedCount != 0 {
		log.Printf("===== inserted a new user with ID %v\n", res.UpsertedID)
	}
	return nil
}

func cacheDeleteUser(id string) error {
	if !cacheEnabled {
		return &Error{Message: "Cache is not enabled"}
	}

	filter := bson.D{primitive.E{Key: "id", Value: id}}
	res, err := usersCollection.DeleteOne(context.TODO(), filter)
	if err != nil {
		log.Printf("===== cacheDeleteUser error: %q", err)
		return err
	}
	log.Printf("===== cacheDeleteUser %+v", res)
	return nil
}

func cacheInsertContactMethod(cm *ContactMethod) error {
	if !cacheEnabled {
		return &Error{Message: "Cache is not enabled"}
	}

	res, err := contactMethodsCollection.InsertOne(context.TODO(), &cm)
	if err != nil {
		log.Printf("===== cacheInsertContactMethod error: %q", err)
		return err
	}
	log.Printf("===== cacheInsertContactMethod %+v", res)
	return nil
}

func cacheGetContactMethod(id string, v interface{}) error {
	if !cacheEnabled {
		return &Error{Message: "Cache is not enabled"}
	}
	filter := bson.D{primitive.E{Key: "id", Value: id}}
	r := contactMethodsCollection.FindOne(context.TODO(), filter)
	err := r.Decode(v)

	if err != nil {
		return err
	}
	return nil
}

func cacheUpdateContactMethod(cm *ContactMethod) error {
	if !cacheEnabled {
		return &Error{Message: "Cache is not enabled"}
	}

	filter := bson.D{primitive.E{Key: "id", Value: cm.ID}}
	opts := options.Replace().SetUpsert(true)
	res, err := contactMethodsCollection.ReplaceOne(context.TODO(), filter, &cm, opts)
	if err != nil {
		log.Printf("===== Error updating contact method: %q", err)
		return err
	}
	if res.MatchedCount != 0 {
		log.Println("===== replaced an existing contact method")
		return nil
	}
	if res.UpsertedCount != 0 {
		log.Printf("===== inserted a new contact method with ID %v\n", res.UpsertedID)
	}
	return nil
}

func cacheDeleteContactMethod(id string) error {
	if !cacheEnabled {
		return &Error{Message: "Cache is not enabled"}
	}

	filter := bson.D{primitive.E{Key: "id", Value: id}}
	res, err := contactMethodsCollection.DeleteOne(context.TODO(), filter)
	if err != nil {
		log.Printf("===== cacheDeleteContactMethod error: %q", err)
		return err
	}
	log.Printf("===== cacheDeleteContactMethod %+v", res)
	return nil
}
