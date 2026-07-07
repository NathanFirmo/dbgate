package mongo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"dbgate/internal/config"
)

var (
	ErrUnknownDatabase = errors.New("unknown mongo database")
	ErrInvalidRequest  = errors.New("invalid mongo request")
)

type ExecRequest struct {
	Collection string           `json:"collection"`
	Op         string           `json:"op"`
	Filter     map[string]any   `json:"filter"`
	Document   map[string]any   `json:"document"`
	Documents  []map[string]any `json:"documents"`
	Update     map[string]any   `json:"update"`
}

type QueryRequest struct {
	Collection string         `json:"collection"`
	Filter     map[string]any `json:"filter"`
}

type Executor struct {
	databases map[string]*mongo.Database
	clients   map[string]*mongo.Client
}

func New(databases []config.Database) (*Executor, error) {
	executor := &Executor{
		databases: make(map[string]*mongo.Database),
		clients:   make(map[string]*mongo.Client),
	}

	for _, database := range databases {
		if database.Type != "mongo" {
			continue
		}

		client, ok := executor.clients[database.URI]
		if !ok {
			var err error
			client, err = mongo.Connect(
				context.Background(),
				options.Client().
					ApplyURI(database.URI).
					SetServerSelectionTimeout(1500*time.Millisecond),
			)
			if err != nil {
				_ = executor.Close(context.Background())
				return nil, fmt.Errorf("connect %s: %w", database.Identifier(), err)
			}
			executor.clients[database.URI] = client
		}
		executor.databases[database.Name] = client.Database(database.Database)
	}

	return executor, nil
}

func (e *Executor) Exec(ctx context.Context, name string, request ExecRequest) (map[string]any, error) {
	collection, err := e.collection(name, request.Collection)
	if err != nil {
		return nil, err
	}

	switch strings.ToLower(strings.TrimSpace(request.Op)) {
	case "deletemany":
		result, err := collection.DeleteMany(ctx, normalizeDocument(request.Filter))
		if err != nil {
			return nil, fmt.Errorf("delete many: %w", err)
		}
		return map[string]any{"ok": true, "deleted": result.DeletedCount}, nil
	case "deleteone":
		result, err := collection.DeleteOne(ctx, normalizeDocument(request.Filter))
		if err != nil {
			return nil, fmt.Errorf("delete one: %w", err)
		}
		return map[string]any{"ok": true, "deleted": result.DeletedCount}, nil
	case "insertone":
		if len(request.Document) == 0 {
			return nil, fmt.Errorf("%w: document is required for insertOne", ErrInvalidRequest)
		}
		result, err := collection.InsertOne(ctx, request.Document)
		if err != nil {
			return nil, fmt.Errorf("insert one: %w", err)
		}
		return map[string]any{"ok": true, "insertedId": result.InsertedID}, nil
	case "insertmany":
		if len(request.Documents) == 0 {
			return nil, fmt.Errorf("%w: documents are required for insertMany", ErrInvalidRequest)
		}
		documents := make([]any, 0, len(request.Documents))
		for _, document := range request.Documents {
			documents = append(documents, document)
		}
		result, err := collection.InsertMany(ctx, documents)
		if err != nil {
			return nil, fmt.Errorf("insert many: %w", err)
		}
		return map[string]any{"ok": true, "inserted": len(result.InsertedIDs)}, nil
	case "updateone":
		if len(request.Update) == 0 {
			return nil, fmt.Errorf("%w: update is required for updateOne", ErrInvalidRequest)
		}
		result, err := collection.UpdateOne(ctx, normalizeDocument(request.Filter), normalizeDocument(request.Update))
		if err != nil {
			return nil, fmt.Errorf("update one: %w", err)
		}
		return map[string]any{
			"ok":       true,
			"matched":  result.MatchedCount,
			"modified": result.ModifiedCount,
		}, nil
	case "updatemany":
		if len(request.Update) == 0 {
			return nil, fmt.Errorf("%w: update is required for updateMany", ErrInvalidRequest)
		}
		result, err := collection.UpdateMany(ctx, normalizeDocument(request.Filter), normalizeDocument(request.Update))
		if err != nil {
			return nil, fmt.Errorf("update many: %w", err)
		}
		return map[string]any{
			"ok":       true,
			"matched":  result.MatchedCount,
			"modified": result.ModifiedCount,
		}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported op %q", ErrInvalidRequest, request.Op)
	}
}

func (e *Executor) Query(ctx context.Context, name string, request QueryRequest) ([]map[string]any, error) {
	collection, err := e.collection(name, request.Collection)
	if err != nil {
		return nil, err
	}

	cursor, err := collection.Find(ctx, normalizeDocument(request.Filter))
	if err != nil {
		return nil, fmt.Errorf("find documents: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []map[string]any
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode documents: %w", err)
	}

	return docs, nil
}

func (e *Executor) Close(ctx context.Context) error {
	var closeErr error
	for _, client := range e.clients {
		if err := client.Disconnect(ctx); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (e *Executor) collection(name string, collection string) (*mongo.Collection, error) {
	if strings.TrimSpace(collection) == "" {
		return nil, fmt.Errorf("%w: collection is required", ErrInvalidRequest)
	}

	database, ok := e.databases[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownDatabase, name)
	}

	return database.Collection(collection), nil
}

func normalizeDocument(document map[string]any) bson.M {
	if len(document) == 0 {
		return bson.M{}
	}
	return bson.M(document)
}
