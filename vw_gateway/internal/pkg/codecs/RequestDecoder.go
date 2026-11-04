package codecs

import (
	"bytes"
	stderr "errors"
	"fmt"
	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/transport/http"
	"io"
	nethttp "net/http"
	"reflect"
	"strconv"
	"strings"
)

const maxMemory = 32 << 20 // 32MB
type formDataValue map[string][]string

func RequestDecoder(r *http.Request, v interface{}) error {
	if r.Method == nethttp.MethodPut && r.URL.Path == "/api/v1/user/profile/avatar" {
		// 在 multipart 解析前限制总请求体，避免头像上传先把超大文件写入临时目录。
		// 文件最多 5 MiB，额外 64 KiB 留给 multipart 边界和表单元信息。
		r.Body = nethttp.MaxBytesReader(nil, r.Body, (5<<20)+(64<<10))
	}
	if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
		err := r.ParseMultipartForm(maxMemory)
		if err != nil {
			var tooLarge *nethttp.MaxBytesError
			if stderr.As(err, &tooLarge) {
				return errors.New(nethttp.StatusRequestEntityTooLarge, "AVATAR_TOO_LARGE", "头像请求体过大")
			}
			return errors.BadRequest("ParseMultipartForm ERROR", err.Error())
		}

		err = bindFormDataByJSONTag(r.MultipartForm.Value, v)
		if err != nil {
			return errors.BadRequest("BIND", err.Error())
		}
		return nil
	}

	codec, ok := http.CodecForRequest(r, "Content-Type")
	if !ok {
		return errors.BadRequest("CODEC", fmt.Sprintf("unregister Content-Type: %s", r.Header.Get("Content-Type")))
	}
	data, err := io.ReadAll(r.Body)

	// reset body.
	r.Body = io.NopCloser(bytes.NewBuffer(data))
	if err != nil {
		return errors.BadRequest("CODEC", err.Error())
	}
	if len(data) == 0 {
		return nil
	}
	if err = codec.Unmarshal(data, v); err != nil {
		return errors.BadRequest("CODEC", fmt.Sprintf("body unmarshal %s", err.Error()))
	}
	return nil
}

// bindFormDataByJSONTag 根据json标签绑定表单值到结构体字段
func bindFormDataByJSONTag(value formDataValue, v interface{}) error {
	val := reflect.ValueOf(v).Elem()
	valType := val.Type()
	if val.Kind() != reflect.Struct {
		return errors.BadRequest("BIND", "v must be a struct")
	}
	//遍历结构体字段，并根据结构体字段的json标签去表单值中查找对应的值，并绑定到结构体字段
	for i := 0; i < val.NumField(); i++ {
		fieldValue := val.Field(i)
		//获取json标签
		fieldTag := valType.Field(i).Tag.Get("json")

		// 将标签分割为两部分，第一部分标识字段名，第二部分为其他参数
		tag := strings.SplitN(fieldTag, ",", 2)

		//使用json标签的第一部分作为key查找表单值
		if formValue, ok := value[tag[0]]; ok {
			fieldKind := fieldValue.Kind()
			switch fieldKind {
			case reflect.String:
				fieldValue.SetString(formValue[0])
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				intVal, err := strconv.Atoi(formValue[0])
				if err == nil {
					fieldValue.SetInt(int64(intVal))
				}
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				uintVal, err := strconv.ParseUint(formValue[0], 10, 64)
				if err == nil {
					fieldValue.SetUint(uintVal)
				}
			case reflect.Float32, reflect.Float64:
				floatVal, err := strconv.ParseFloat(formValue[0], 64)
				if err == nil {
					fieldValue.SetFloat(floatVal)
				}
			case reflect.Bool:
				boolVal, err := strconv.ParseBool(formValue[0])
				if err == nil {
					fieldValue.SetBool(boolVal)
				}
			default:
			}
		}
	}
	return nil
}

// bindFormDataByFieldName 根据字段名绑定表单值到结构体字段
func bindFormDataByFieldName(value formDataValue, v interface{}) error {
	val := reflect.ValueOf(v).Elem()
	if val.Kind() != reflect.Struct {
		return errors.BadRequest("BIND", "v must be a struct")
	}
	//遍历表单值并绑定到结构体
	for key, Values := range value {
		fieldValue := val.FieldByName(firstUpper(key))
		fieldKind := fieldValue.Kind()
		switch fieldKind {
		case reflect.String:
			fieldValue.SetString(Values[0])
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			intVal, err := strconv.Atoi(Values[0])
			if err == nil {
				fieldValue.SetInt(int64(intVal))
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			uintVal, err := strconv.ParseUint(Values[0], 10, 64)
			if err == nil {
				fieldValue.SetUint(uintVal)
			}
		case reflect.Float32, reflect.Float64:
			floatVal, err := strconv.ParseFloat(Values[0], 64)
			if err == nil {
				fieldValue.SetFloat(floatVal)
			}
		case reflect.Bool:
			boolVal, err := strconv.ParseBool(Values[0])
			if err == nil {
				fieldValue.SetBool(boolVal)
			}
		default:

		}
	}

	return nil
}

// firstUpper 字符串首字母大写
func firstUpper(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
