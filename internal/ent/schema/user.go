package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// User 用户实体，对应表 app_user
type User struct {
	ent.Schema
}

func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Table("app_user"),
	}
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.Uint("id").
			StorageKey("id").
			StructTag(`json:"id"`),
		field.String("name").
			MaxLen(100).
			Default("").
			Optional().
			StructTag(`json:"name"`),
		field.Int("age").
			Default(0).
			Optional().
			StructTag(`json:"age"`),
		field.String("email").
			MaxLen(255).
			Default("").
			Optional().
			StructTag(`json:"email"`),
		field.String("account").
			MaxLen(100).
			NotEmpty().
			Unique().
			StructTag(`json:"account"`),
		field.String("passwd").
			MaxLen(255).
			NotEmpty().
			StructTag(`json:"-"`),
		field.String("role").
			MaxLen(50).
			Default("user").Optional().
			StructTag(`json:"role"`),
		field.String("status").
			MaxLen(50).
			Default("active").Optional().
			StructTag(`json:"status"`),
		field.Time("create_time").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Default(time.Now).
			Optional().
			StructTag(`json:"createTime"`),
		field.Time("update_time").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Default(time.Now).
			UpdateDefault(time.Now).
			Optional().
			StructTag(`json:"updateTime"`),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("account").Unique(),
	}
}
