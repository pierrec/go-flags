package flags

import (
	"reflect"
	"unicode/utf8"
	"unsafe"
	"strings"
)

type scanHandler func (reflect.Value, *reflect.StructField) (bool, error)

func newGroup(shortDescription string, longDescription string, data interface{}) *Group {
	return &Group{
		ShortDescription: shortDescription,
		LongDescription:  longDescription,

		data: data,
	}
}

func (g *Group) optionByName(name string, namematch func(*Option, string) bool) *Option {
	prio := 0
	var retopt *Option

	for _, opt := range g.options {
		if namematch != nil && namematch(opt, name) && prio < 4 {
			retopt = opt
			prio = 4
		}

		if name == opt.field.Name && prio < 3 {
			retopt = opt
			prio = 3
		}

		if name == opt.LongName && prio < 2 {
			retopt = opt
			prio = 2
		}

		if opt.ShortName != 0 && name == string(opt.ShortName) && prio < 1 {
			retopt = opt
			prio = 1
		}
	}

	return retopt
}

func (g *Group) storeDefaults() {
	for _, option := range g.options {
		// First. empty out the value
		if len(option.Default) > 0 {
			option.clear()
		}

		for _, d := range option.Default {
			option.set(&d)
		}

		if !option.value.CanSet() {
			continue
		}

		option.defaultValue = reflect.ValueOf(option.value.Interface())
	}
}

func (g *Group) eachGroup(f func(*Group), recurse bool) {
	f(g)

	for _, gg := range g.groups {
		if recurse {
			gg.eachGroup(f, true)
		} else {
			f(gg)
		}
	}
}

func (g *Group) scanStruct(realval reflect.Value, sfield *reflect.StructField, handler scanHandler) error {
	stype := realval.Type()

	if sfield != nil {
		if ok, err := handler(realval, sfield); err != nil {
			return err
		} else if ok {
			return nil
		}
	}

	for i := 0; i < stype.NumField(); i++ {
		field := stype.Field(i)

		// PkgName is set only for non-exported fields, which we ignore
		if field.PkgPath != "" {
			continue
		}

		mtag := newMultiTag(string(field.Tag))

		// Skip fields with the no-flag tag
		if mtag.Get("no-flag") != "" {
			continue
		}

		// Dive deep into structs or pointers to structs
		kind := field.Type.Kind()

		if kind == reflect.Struct {
			return g.scanStruct(realval.Field(i), &field, handler)
		} else if kind == reflect.Ptr && field.Type.Elem().Kind() == reflect.Struct && !realval.Field(i).IsNil() {
			return g.scanStruct(reflect.Indirect(realval.Field(i)), &field, handler)
		}

		longname := mtag.Get("long")
		shortname := mtag.Get("short")

		// Need at least either a short or long name
		if longname == "" && shortname == "" {
			continue
		}

		short := rune(0)
		rc := utf8.RuneCountInString(shortname)

		if rc > 1 {
			return ErrShortNameTooLong
		} else if rc == 1 {
			short, _ = utf8.DecodeRuneInString(shortname)
		}

		description := mtag.Get("description")
		def := mtag.GetMany("default")
		optionalValue := mtag.GetMany("optional-value")
		valueName := mtag.Get("value-name")
		defaultMask := mtag.Get("default-mask")
		ininame := mtag.Get("ini-name")

		optional := (mtag.Get("optional") != "")
		required := (mtag.Get("required") != "")

		option := &Option{
			Description:      description,
			ShortName:        short,
			LongName:         longname,
			Default:          def,
			OptionalArgument: optional,
			OptionalValue:    optionalValue,
			Required:         required,
			ValueName:        valueName,
			DefaultMask:      defaultMask,
			IniName:          ininame,

			field:            field,
			value:            realval.Field(i),
			tag:              mtag,
		}

		g.options = append(g.options, option)
	}

	return nil
}

func (g *Group) scanSubGroupHandler(realval reflect.Value, sfield *reflect.StructField) (bool, error) {
	mtag := newMultiTag(string(sfield.Tag))

	subgroup := mtag.Get("group")

	if len(subgroup) != 0 {
		ptrval := reflect.NewAt(realval.Type(), unsafe.Pointer(realval.UnsafeAddr()))
		description := mtag.Get("description")

		if _, err := g.AddGroup(subgroup, description, ptrval.Interface()); err != nil {
			return true, err
		}

		return true, nil
	}

	return false, nil
}

func (g *Group) scanType(handler scanHandler) error {
	// Get all the public fields in the data struct
	ptrval := reflect.ValueOf(g.data)

	if ptrval.Type().Kind() != reflect.Ptr {
		panic(ErrNotPointerToStruct)
	}

	stype := ptrval.Type().Elem()

	if stype.Kind() != reflect.Struct {
		panic(ErrNotPointerToStruct)
	}

	realval := reflect.Indirect(ptrval)

	return g.scanStruct(realval, nil, handler)
}

func (g *Group) scan() error {
	return g.scanType(g.scanSubGroupHandler)
}

func (g *Group) groupByName(name string) *Group {
	name = strings.ToLower(name)

	if len(name) == 0 {
		return g
	}

	for _, subg := range g.groups {
		lname := strings.ToLower(subg.ShortDescription)
		prefix := lname + "."

		if strings.HasPrefix(name, prefix) {
			if grp := subg.groupByName(name[len(prefix):]); grp != nil {
				return grp
			}
		} else if name == lname {
			return subg
		}
	}

	return nil
}
