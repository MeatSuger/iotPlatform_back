package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// User 用户实体，对应表 app_user
type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.Uint("id").
			StorageKey("id").
			StructTag(`json:"id"`),
		field.String("name").
			MaxLen(100).
			Default("").
			StructTag(`json:"name"`),
		field.Int("age").
			Default(0).
			StructTag(`json:"age"`),
		field.String("email").
			MaxLen(255).
			Default("").
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
			Default("user").
			StructTag(`json:"role"`),
		field.String("status").
			MaxLen(50).
			Default("active").
			StructTag(`json:"status"`),
		field.Time("create_time").
			Default(time.Now).
			StructTag(`json:"createTime"`),
		field.Time("update_time").
			Default(time.Now).
			UpdateDefault(time.Now).
			StructTag(`json:"updateTime"`),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("account").Unique(),
	}
}

func (User) Table() string {
	return "app_user"
}
