package mongoapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	Client *mongo.Client = nil
	dbName string
)

func SetMongoDB(setdbName string, url string) error {
	if Client != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(url))
	if err != nil {
		return fmt.Errorf("SetMongoDB err: %+v", err)
	}
	Client = client
	dbName = setdbName
	return nil
}

func findOneAndDecode(collection *mongo.Collection, filter bson.M) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := collection.FindOne(context.TODO(), filter).Decode(&result); err != nil {
		// ErrNoDocuments means that the filter did not match any documents in
		// the collection.
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return result, nil
}

func getOrigData(collection *mongo.Collection, filter bson.M) (map[string]interface{}, error) {
	result, err := findOneAndDecode(collection, filter)
	if err != nil {
		return nil, err
	}
	if result != nil {
		// Delete "_id" entry which is auto-inserted by MongoDB
		delete(result, "_id")
	}
	return result, nil
}

func checkDataExisted(collection *mongo.Collection, filter bson.M) (bool, error) {
	result, err := findOneAndDecode(collection, filter)
	if err != nil {
		return false, err
	}
	if result == nil {
		return false, nil
	}
	return true, nil
}

func RestfulAPIGetOne(collName string, filter bson.M) (map[string]interface{}, error) {
	collection := Client.Database(dbName).Collection(collName)
	result, err := getOrigData(collection, filter)
	if err != nil {
		return nil, fmt.Errorf("RestfulAPIGetOne err: %+v", err)
	}
	return result, nil
}

func RestfulAPIGetMany(collName string, filter bson.M) ([]map[string]interface{}, error) {
	collection := Client.Database(dbName).Collection(collName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cur, err := collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("RestfulAPIGetMany err: %+v", err)
	}
	defer func(ctx context.Context) {
		if err := cur.Close(ctx); err != nil {
			return
		}
	}(ctx)

	var resultArray []map[string]interface{}
	for cur.Next(ctx) {
		var result map[string]interface{}
		if err := cur.Decode(&result); err != nil {
			return nil, fmt.Errorf("RestfulAPIGetMany err: %+v", err)
		}

		// Delete "_id" entry which is auto-inserted by MongoDB
		delete(result, "_id")
		resultArray = append(resultArray, result)
	}
	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("RestfulAPIGetMany err: %+v", err)
	}

	return resultArray, nil
}

// if no error happened, return true means data existed and false means data not existed
func RestfulAPIPutOne(collName string, filter bson.M, putData map[string]interface{}) (bool, error) {
	collection := Client.Database(dbName).Collection(collName)
	existed, err := checkDataExisted(collection, filter)
	if err != nil {
		return false, fmt.Errorf("RestfulAPIPutOne err: %+v", err)
	}

	if existed {
		if _, err := collection.UpdateOne(context.TODO(), filter, bson.M{"$set": putData}); err != nil {
			return false, fmt.Errorf("RestfulAPIPutOne UpdateOne err: %+v", err)
		}
		return true, nil
	}

	if _, err := collection.InsertOne(context.TODO(), putData); err != nil {
		return false, fmt.Errorf("RestfulAPIPutOne InsertOne err: %+v", err)
	}
	return false, nil
}

// if no error happened, return true means data existed (not updated) and false means data not existed
func RestfulAPIPutOneNotUpdate(collName string, filter bson.M, putData map[string]interface{}) (bool, error) {
	collection := Client.Database(dbName).Collection(collName)
	existed, err := checkDataExisted(collection, filter)
	if err != nil {
		return false, fmt.Errorf("RestfulAPIPutOneNotUpdate err: %+v", err)
	}

	if existed {
		return true, nil
	}

	if _, err := collection.InsertOne(context.TODO(), putData); err != nil {
		return false, fmt.Errorf("RestfulAPIPutOneNotUpdate InsertOne err: %+v", err)
	}
	return false, nil
}

func RestfulAPIPutMany(collName string, filterArray []bson.M, putDataArray []map[string]interface{}) error {
	collection := Client.Database(dbName).Collection(collName)

	for i, putData := range putDataArray {
		filter := filterArray[i]
		existed, err := checkDataExisted(collection, filter)
		if err != nil {
			return fmt.Errorf("RestfulAPIPutMany err: %+v", err)
		}

		if existed {
			if _, err := collection.UpdateOne(context.TODO(), filter, bson.M{"$set": putData}); err != nil {
				return fmt.Errorf("RestfulAPIPutMany UpdateOne err: %+v", err)
			}
		} else {
			if _, err := collection.InsertOne(context.TODO(), putData); err != nil {
				return fmt.Errorf("RestfulAPIPutMany InsertOne err: %+v", err)
			}
		}
	}
	return nil
}

func RestfulAPIDeleteOne(collName string, filter bson.M) error {
	collection := Client.Database(dbName).Collection(collName)

	if _, err := collection.DeleteOne(context.TODO(), filter); err != nil {
		return fmt.Errorf("RestfulAPIDeleteOne err: %+v", err)
	}
	return nil
}

func RestfulAPIDeleteMany(collName string, filter bson.M) error {
	collection := Client.Database(dbName).Collection(collName)

	if _, err := collection.DeleteMany(context.TODO(), filter); err != nil {
		return fmt.Errorf("RestfulAPIDeleteMany err: %+v", err)
	}
	return nil
}

func RestfulAPIMergePatch(collName string, filter bson.M, patchData map[string]interface{}) error {
	collection := Client.Database(dbName).Collection(collName)

	originalData, err := getOrigData(collection, filter)
	if err != nil {
		return fmt.Errorf("RestfulAPIMergePatch getOrigData err: %+v", err)
	}

	original, err := json.Marshal(originalData)
	if err != nil {
		return fmt.Errorf("RestfulAPIMergePatch Marshal err: %+v", err)
	}

	patchDataByte, err := json.Marshal(patchData)
	if err != nil {
		return fmt.Errorf("RestfulAPIMergePatch Marshal err: %+v", err)
	}

	modifiedAlternative, err := jsonpatch.MergePatch(original, patchDataByte)
	if err != nil {
		return fmt.Errorf("RestfulAPIMergePatch MergePatch err: %+v", err)
	}

	var modifiedData map[string]interface{}
	if err := json.Unmarshal(modifiedAlternative, &modifiedData); err != nil {
		return fmt.Errorf("RestfulAPIMergePatch Unmarshal err: %+v", err)
	}
	if _, err := collection.UpdateOne(context.TODO(), filter, bson.M{"$set": modifiedData}); err != nil {
		return fmt.Errorf("RestfulAPIMergePatch UpdateOne err: %+v", err)
	}
	return nil
}

func RestfulAPIJSONPatch(collName string, filter bson.M, patchJSON []byte) error {
	collection := Client.Database(dbName).Collection(collName)

	originalData, err := getOrigData(collection, filter)
	if err != nil {
		return fmt.Errorf("RestfulAPIJSONPatch getOrigData err: %+v", err)
	}

	original, err := json.Marshal(originalData)
	if err != nil {
		return fmt.Errorf("RestfulAPIJSONPatch Marshal err: %+v", err)
	}

	patch, err := jsonpatch.DecodePatch(patchJSON)
	if err != nil {
		return fmt.Errorf("RestfulAPIJSONPatch DecodePatch err: %+v", err)
	}

	modified, err := patch.Apply(original)
	if err != nil {
		return fmt.Errorf("RestfulAPIJSONPatch Apply err: %+v", err)
	}

	var modifiedData map[string]interface{}
	if err := json.Unmarshal(modified, &modifiedData); err != nil {
		return fmt.Errorf("RestfulAPIJSONPatch Unmarshal err: %+v", err)
	}
	if _, err := collection.UpdateOne(context.TODO(), filter, bson.M{"$set": modifiedData}); err != nil {
		return fmt.Errorf("RestfulAPIJSONPatch UpdateOne err: %+v", err)
	}
	return nil
}

func RestfulAPIJSONPatchExtend(collName string, filter bson.M, patchJSON []byte, dataName string) error {
	collection := Client.Database(dbName).Collection(collName)

	originalDataCover, err := getOrigData(collection, filter)
	if err != nil {
		return fmt.Errorf("RestfulAPIJSONPatchExtend getOrigData err: %+v", err)
	}

	originalData := originalDataCover[dataName]
	original, err := json.Marshal(originalData)
	if err != nil {
		return fmt.Errorf("RestfulAPIJSONPatchExtend Marshal err: %+v", err)
	}

	patch, err := jsonpatch.DecodePatch(patchJSON)
	if err != nil {
		return fmt.Errorf("RestfulAPIJSONPatchExtend DecodePatch err: %+v", err)
	}

	modified, err := patch.Apply(original)
	if err != nil {
		return fmt.Errorf("RestfulAPIJSONPatchExtend Apply err: %+v", err)
	}

	var modifiedData map[string]interface{}
	if err := json.Unmarshal(modified, &modifiedData); err != nil {
		return fmt.Errorf("RestfulAPIJSONPatchExtend Unmarshal err: %+v", err)
	}
	if _, err := collection.UpdateOne(context.TODO(), filter, bson.M{"$set": bson.M{dataName: modifiedData}}); err != nil {
		return fmt.Errorf("RestfulAPIJSONPatchExtend UpdateOne err: %+v", err)
	}
	return nil
}

func RestfulAPIPost(collName string, filter bson.M, postData map[string]interface{}) (bool, error) {
	return RestfulAPIPutOne(collName, filter, postData)
}

func RestfulAPIPostMany(collName string, filter bson.M, postDataArray []interface{}) error {
	collection := Client.Database(dbName).Collection(collName)

	if _, err := collection.InsertMany(context.TODO(), postDataArray); err != nil {
		return fmt.Errorf("RestfulAPIPostMany err: %+v", err)
	}
	return nil
}

func Drop(collName string) error {
	collection := Client.Database(dbName).Collection(collName)
	return collection.Drop(context.TODO())
}
