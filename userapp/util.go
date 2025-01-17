package userapp

import (
	"encoding/base64"
	"fmt"
	"github.com/go-redis/redis"
	jsoniter "github.com/json-iterator/go"
	. "github.com/leyle/ginbase/consolelog"
	"github.com/leyle/ginbase/dbandmq"
	"github.com/leyle/ginbase/util"
	"strconv"
	"strings"
	"time"
)

var AesKey = util.Md5("www.hbbclub.com") // 32 byte 使用加密方法就是 aes-256-cfb

// 生成 token
// 使用 aes-256-cfb 加密来生成 token
func GenerateToken(userId string) (string, error) {
	t := time.Now().Unix()
	text := fmt.Sprintf("%s|%d", userId, t)

	token, err := util.Encrypt([]byte(AesKey), text)
	if err != nil {
		Logger.Errorf("", "给用户[%s]生成token时，调用aes加密失败, %s", userId, err.Error())
		return "", err
	}

	// 在用 base64 编码
	b64Token := base64.StdEncoding.EncodeToString([]byte(token))

	return b64Token, nil
}

// 解析 token
func ParseToken(token string) (string, int64, error) {
	// 先 base64 解码
	de64Token, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		Logger.Errorf("", "base64解码token[%s]失败, %s", token, err.Error())
		return "", 0, err
	}

	// 再 aes 解密
	text, err := util.Decrypt([]byte(AesKey), string(de64Token))
	if err != nil {
		Logger.Errorf("", "aes解密token[%s]失败, %s", de64Token, err.Error())
		return "", 0, err
	}
	infos := strings.Split(text, "|")
	userId := infos[0]
	st := infos[1]

	t, _ := strconv.ParseInt(st, 10, 64)

	return userId, t, nil
}

// 存储token
// 存储为 key 是 userid， 值是 tokenvalue
func SaveToken(r *redis.Client, token string, user *User) error {
	tkVal := &TokenVal{
		Token: token,
		User:  user,
		T:     util.GetCurTime(),
	}

	tkDump, _ := jsoniter.Marshal(&tkVal)

	key := TokenRedisPrefix + user.Id
	_, err := r.Set(key, tkDump, 0).Result()
	if err != nil {
		Logger.Errorf("", "存储用户[%s]的token到redis失败, %s", user.Id, err.Error())
		return err
	}

	return nil
}

// 删除token
func DeleteToken(r *redis.Client, userId string) error {
	key := TokenRedisPrefix + userId
	_, err := r.Del(key).Result()
	if err != nil && err != redis.Nil {
		Logger.Errorf("", "移除用户[%s]token失败, %s", userId, err.Error())
		return err
	}

	Logger.Infof("", "移除用户[%s]token成功", userId)
	return nil
}

// 验证 token
func CheckToken(r *redis.Client, token string) (*TokenVal, error) {
	// 先解析 token
	userId, t, err := ParseToken(token)
	if err != nil {
		return nil, err
	}
	Logger.Debugf("", "CheckToken 时，parsetoken成功，用户[%s]，token生成时间[%s]", userId, util.FmtTimestampTime(t))

	// 从 redis 中读取 tokenval 信息
	key := TokenRedisPrefix + userId
	data, err := r.Get(key).Result()
	if err != nil {
		Logger.Errorf("", "CheckToken 时，从redis读取指定用户[%s]的tokenval失败, %s", userId, err.Error())
		return nil, err
	}

	var tkVal *TokenVal
	err = jsoniter.UnmarshalFromString(data, &tkVal)
	if err != nil {
		Logger.Errorf("", "CheckToken 时，反序列化从 redis 读取回来的用户[%s]的数据失败, %s", userId, err.Error())
		return nil, err
	}

	return tkVal, nil
}

// 确保系统启动时包含了系统管理员账户
func InsureAdminAccount(db *dbandmq.Ds) (*User, error) {
	user, err := GetUserByLoginId(db, AdminLoginId)
	if err != nil {
		return nil, err
	}

	if user == nil {
		// 新建初始化账户
		user, err = initAdminAccount(db)
		if err != nil {
			return nil, err
		}
	}

	Logger.Infof("", "启动userapp，init admin 账户成功，userId[%s]", user.Id)

	return user, nil
}

func initAdminAccount(db *dbandmq.Ds) (*User, error) {
	salt := util.GenerateDataId()
	p := AdminLoginPasswd + salt
	hashP := util.Sha256(p)

	user := &User{
		Id:        util.GenerateDataId(),
		Name:      AdminLoginId,
		CreateT:   util.GetCurTime(),
	}
	user.UpdateT = user.CreateT

	ulpa := &UserLoginIdPasswdAuth{
		Id:      util.GenerateDataId(),
		UserId:  user.Id,
		LoginId: AdminLoginId,
		Salt:    salt,
		Passwd:  hashP,
		Init: true,
		CreateT: user.CreateT,
		UpdateT: user.CreateT,
	}

	err := db.C(CollectionNameUser).Insert(user)
	if err != nil {
		Logger.Errorf("", "初始化系统admin账户，存储user表失败, %s", err.Error())
		return nil, err
	}

	err = db.C(CollectionNameIdPasswd).Insert(ulpa)
	if err != nil {
		Logger.Errorf("", "初始化系统admin账户，存储账户密码表失败, %s", err.Error())
		return nil, err
	}

	user.LoginType = LoginTypeIdPasswd
	user.IdPasswd = ulpa

	Logger.Infof("", "初始化系统admin账户成功，用户id[%s]", user.Id)
	return user, nil
}