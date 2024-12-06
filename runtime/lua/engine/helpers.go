package engine

import (
	"github.com/ponyruntime/go-lua"
	"go.uber.org/zap"
)

func TableToSlice(v lua.LValue, log *zap.Logger) []string {
	var ret []string

	if v.Type() != lua.LTTable {
		log.Warn("cannot parse table", zap.String("type", v.Type().String()))
		return nil
	}

	ToTable(v).ForEach(func(_, value lua.LValue) {
		ret = append(ret, value.String())
	})

	return ret
}

func TableToAnySlice(v lua.LValue, log *zap.Logger) []any {
	var ret []any

	if v.Type() != lua.LTTable {
		log.Warn("cannot parse table", zap.String("type", v.Type().String()))
		return nil
	}

	ToTable(v).ForEach(func(_, value lua.LValue) {
		ret = append(ret, ToGoAny(value))
	})

	return ret
}

func TableToMap(t *lua.LTable, log *zap.Logger) map[string]string {
	if t == nil {
		log.Warn("table key exists, but the underlying table is nil")
		return nil
	}

	var ret = make(map[string]string)

	t.ForEach(func(key, val lua.LValue) {
		ret[key.String()] = val.String()
	})

	return ret
}

func ToTable(v lua.LValue) *lua.LTable {
	if lv, ok := v.(*lua.LTable); ok {
		return lv
	}
	return nil
}
