package littleorm

import (
	"errors"
	"fmt"
	"log"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

var (
	host     = "http://127.0.0.1"
	port     = 62894
	user     = "root"
	password = "123"
	dbname   = "name"
)

var db *DB

const (
	tablename = "little_orm"
	name      = "allen"
	age       = 18
)

type LittleOrm struct {
	Id        uint64    `db:"id" json:"id"`
	Name      string    `db:"name"`
	Age       int8      `db:"age"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

func init() {
	dataSourceName := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&loc=%s&parseTime=true", user, password, host, port, dbname, "Asia%2FShanghai")
	var err error
	db, err = Open("mysql", dataSourceName, 10*time.Second)
	if err != nil {
		fmt.Printf("open conn err: %v", err)
	}

	sql := `CREATE TABLE little_orm (
		id int(11) unsigned NOT NULL AUTO_INCREMENT,
		name varchar(32) NOT NULL DEFAULT '',
		age int(11) NOT NULL,
		created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		PRIMARY KEY (id)
	  ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`

	_, err = db.Acquire().Name(tablename).Drop()
	if err != nil {
		log.Fatalf("drop table failed, err: %v", err)
	}
	_, err = db.Acquire().Create(sql)
	if err != nil {
		log.Fatalf("create table failed, err: %v", err)
	}
}

func TestInsert(t *testing.T) {
	data := map[string]interface{}{
		"name": name,
		"age":  age,
	}
	result, err := db.Acquire().Name(tablename).Insert(data)
	assert.Equal(t, nil, err)
	id, err := result.LastInsertId()
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 1, id)
}

func TestGet(t *testing.T) {
	var (
		little LittleOrm
		err    error
	)
	query := fmt.Sprintf("select * from %s where id=?", tablename)
	err = db.Acquire().Get(&little, query, 1)
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 1, little.Id)
}

func TestInsertBatch(t *testing.T) {
	fields := []string{"name", "age"}
	data := [][]interface{}{
		{name, age + 2},
		{fmt.Sprintf("%s-%d", name, 3), age + 3},
	}
	result, err := db.Acquire().Name(tablename).InsertBatch(fields, data...)
	assert.Equal(t, nil, err)
	rows, err := result.RowsAffected()
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 2, rows)
}

func TestSelect(t *testing.T) {
	var (
		littles []LittleOrm
		err     error
	)
	query := fmt.Sprintf("select * from %s", tablename)
	err = db.Acquire().Select(&littles, query)
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 3, len(littles))
}

func TestFindOne(t *testing.T) {
	var (
		little LittleOrm
		err    error
	)
	err = db.Acquire().Name(tablename).What([]string{"id", "name", "age"}).Where("id=?", 1).FindOne(&little)
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 1, little.Id)
	assert.EqualValues(t, name, little.Name)
	assert.EqualValues(t, age, little.Age)
}

func TestFindOneCount(t *testing.T) {
	var (
		total int64
		err   error
	)
	err = db.Acquire().Name(tablename).What([]string{"count(id) as total"}).FindOne(&total)
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 3, total)
}

func TestFindWithoutWhat(t *testing.T) {
	var (
		little LittleOrm
		err    error
	)
	err = db.Acquire().Name(tablename).Where("id=?", 1).FindOne(&little)
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 1, little.Id)
}

func TestFindMany(t *testing.T) {
	var (
		littles []LittleOrm
		err     error
	)
	err = db.Acquire().Name(tablename).FindMany(&littles)
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 3, len(littles))
}
func TestOrder(t *testing.T) {
	var (
		littles []LittleOrm
		err     error
	)
	err = db.Acquire().Name(tablename).What([]string{"id", "name", "age"}).Order("id desc").FindMany(&littles)
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 3, len(littles))
	assert.EqualValues(t, 3, littles[0].Id)
}

func TestGroupHaving(t *testing.T) {
	var (
		ages []int8
		err  error
	)
	err = db.Acquire().Name(tablename).What([]string{"sum(age) as age"}).Group("name").Having("age > ?", age).FindMany(&ages)
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 2, len(ages))
}

func TestWhereIn(t *testing.T) {
	var (
		littles []LittleOrm
		err     error
	)
	err = db.Acquire().Name(tablename).WhereIn("id", []interface{}{1, 2}).FindMany(&littles)
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 2, len(littles))
}

func TestLimit(t *testing.T) {
	var (
		littles []LittleOrm
		err     error
	)
	err = db.Acquire().Name(tablename).WhereIn("id", []interface{}{1, 2, 3}).Offset(1).Limit(2).FindMany(&littles)
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 2, len(littles))
}

func TestExec(t *testing.T) {
	query := fmt.Sprintf("insert into %s (name, age) values (?,?)", tablename)
	result, err := db.Acquire().Exec(query, name+"-exec", age+1)
	assert.Equal(t, nil, err)
	rows, err := result.RowsAffected()
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 1, rows)
}

func TestUpdate(t *testing.T) {
	rows, err := db.Acquire().Name(tablename).Where("id=?", 2).Update("name=?, age=age+?", name+"-update", 2)
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 1, rows)
}

func TestUpdateMap(t *testing.T) {
	data := map[string]interface{}{
		"name": name + "-updatemap",
		"age":  10,
	}
	rows, err := db.Acquire().Name(tablename).Where("id=?", 2).UpdateMap(data)
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 1, rows)
}

func TestDelete(t *testing.T) {
	rows, err := db.Acquire().Name(tablename).Where("id=?", 3).Delete()
	assert.Equal(t, nil, err)
	assert.EqualValues(t, 1, rows)
}

func TestWithTx(t *testing.T) {
	err := db.WithTx(updateAge, 100)
	assert.Equal(t, nil, err)
}
func updateAge(tx *sqlx.Tx, age interface{}) error {
	var (
		little LittleOrm
	)
	err := db.AcquireTx(tx).Name(tablename).Where("id=?", 1).LockX().FindOne(&little)
	if err != nil {
		return err
	}

	rows, err := db.AcquireTx(tx).Name(tablename).Where("id=?", little.Id).Update("age=age+?", age)
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("update row affect error")
	}
	return nil
}
