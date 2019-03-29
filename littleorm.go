package littleorm

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	Grouping    = " and " //where条件拼接
	ParamMarker = "?"     //参数占位符
	DBTag       = "db"    //数据库tag
	SeqComma    = ", "    //字段分隔符
	SeqSpace    = " "     //空格分隔符
)

const (
	SelectTypeOne = iota
	SelectTypeMany
)

// 用单参数，函数内部调用自行转换类型，否则没办法传递，很烦
type FuncTx func(tx *sqlx.Tx, args interface{}) error

func Open(driverName, dataSourceName string, timeout time.Duration) (*DB, error) {
	db, err := sqlx.Open(driverName, dataSourceName)
	if err != nil {
		return nil, err
	}
	res := &DB{
		DB:      db,
		timeout: timeout,
	}
	res.pool.New = func() interface{} {
		return res.allocateContext()
	}
	return res, nil
}

type DB struct {
	*sqlx.DB
	timeout time.Duration
	pool    sync.Pool
}

func (db *DB) allocateContext() *Context {
	return &Context{db: db}
}

// 获取一个`SQL`执行`Context`
func (db *DB) Acquire() *Context {
	// 无需加锁，sync.Pool本身是线程安全的
	ctx := db.pool.Get().(*Context)
	ctx.reset()
	return ctx
}

// 获取一个带有事务`tx`的`SQL`执行`Context`
func (db *DB) AcquireTx(tx *sqlx.Tx) *Context {
	ctx := db.Acquire()
	ctx.tx = tx
	return ctx
}

// 优雅的开启事务
// 只能用装饰器了，相当于注入了一个事务的上下文对象
// 除了可以统一处理开启事务的代码，好像也没看到啥好处，而且还限制了参数的传递，只能传递一个参数，所以多参数就弄成一个对象传递吧
// 返回值也就只有异常，所以如果需要返回什么数据的，就直接搞到异常里面吧，我也不知道怎么搞...
// 最后，不要搞嵌套事务
func (db *DB) WithTx(h FuncTx, args interface{}) (err error) {
	var tx *sqlx.Tx
	tx, err = db.Beginx()
	if err != nil {
		return
	}
	defer func() {
		if err != nil && tx != nil {
			err = tx.Rollback()
		}
	}()

	// 调用外部函数
	if err = h(tx, args); err != nil {
		return
	}

	err = tx.Commit()
	return
}

type Context struct {
	db     *DB
	tx     *sqlx.Tx //事务
	sql    string
	name   string
	what   []string
	wheres []string
	order  string
	group  string
	having string
	limit  int64
	offset int64
	args   []interface{}
	lockX  bool //排他锁
	lockS  bool //共享锁
}

func (ctx *Context) Name(name string) *Context {
	ctx.name = name
	return ctx
}

// 查询字段
// 如果不指定查询字段，默认使用传递的对象中的标签`db`指定的字段，如果没有指定`db`标签则使用`*`代替
// 使用`*`以后增加数据库字段可能会导致老的查询出错，对兼容性不好，可能是`sqlx`这个库的问题
func (ctx *Context) What(what []string) *Context {
	ctx.what = what
	return ctx
}

func (ctx *Context) Where(where string, args ...interface{}) *Context {
	ctx.wheres = append(ctx.wheres, where)
	ctx.args = append(ctx.args, args...)
	return ctx
}

// 指定字段和字段的可取值，自动拼接成 `field in (?,?)` 形式，`args`必须是 `[]interface{}`类型，"严格"的类型系统，蛤...
func (ctx *Context) WhereIn(field string, args []interface{}) *Context {
	n := len(args)
	places := make([]string, n)
	for i := 0; i < n; i++ {
		places[i] = ParamMarker
	}
	inWhere := fmt.Sprintf("%s in (%s)", field, sqljoin(places, SeqComma))
	return ctx.Where(inWhere, args...)
}

func (ctx *Context) Order(order string) *Context {
	ctx.order = order
	return ctx
}

func (ctx *Context) Limit(limit int64) *Context {
	ctx.limit = limit
	return ctx
}

func (ctx *Context) Offset(offset int64) *Context {
	ctx.offset = offset
	return ctx
}

func (ctx *Context) Group(group string) *Context {
	ctx.group = group
	return ctx
}

func (ctx *Context) Having(having string, args ...interface{}) *Context {
	ctx.having = having
	ctx.args = append(ctx.args, args...)
	return ctx
}

// 加排他锁(X锁)，不保证与`LockS`互斥，自己保证
func (ctx *Context) LockX() *Context {
	ctx.lockX = true
	return ctx
}

// 加共享锁(S锁)，不保证与`LockX`互斥，自己保证
func (ctx *Context) LockS() *Context {
	ctx.lockS = true
	return ctx
}

// 查询多条记录，参数传入一个数组的指针，eg: &[]Little
func (ctx *Context) FindMany(dest interface{}) error {
	return ctx.find(dest, SelectTypeMany)
}

// 查询一条记录，参数传入一个对象指针
func (ctx *Context) FindOne(dest interface{}) error {
	return ctx.find(dest, SelectTypeOne)
}

// 插入
func (ctx *Context) Insert(data map[string]interface{}) (sql.Result, error) {
	var (
		fields []string
		params []interface{}
	)
	for k, v := range data {
		fields = append(fields, k)
		params = append(params, v)
	}
	return ctx.InsertBatch(fields, params)
}

// 批量插入
func (ctx *Context) InsertBatch(fields []string, data ...[]interface{}) (sql.Result, error) {
	var (
		params []interface{}
		values []string
	)
	for _, item := range data {
		places := make([]string, len(item))
		for i, v := range item {
			places[i] = ParamMarker
			params = append(params, v)
		}
		values = append(values, fmt.Sprintf("(%s)", sqljoin(places, SeqComma)))
	}

	query := fmt.Sprintf("insert into %s (%s) values %s", ctx.name, sqljoin(fields, SeqComma), sqljoin(values, SeqComma))
	return ctx.exec(query, params...)
}

// 使用map更新
func (ctx *Context) UpdateMap(args map[string]interface{}) (rowsAffected int64, err error) {
	var (
		params []interface{}
		sets   []string
	)
	for k, v := range args {
		params = append(params, v)
		sets = append(sets, fmt.Sprintf("%s=%s", k, ParamMarker))
	}
	sqlset := sqljoin(sets, SeqComma)
	rowsAffected, err = ctx.Update(sqlset, params...)
	return
}

// 更新
func (ctx *Context) Update(sqlset string, args ...interface{}) (rowsAffected int64, err error) {
	template := "update %s set %s %s"
	where := sqlwhere(ctx.wheres, Grouping)
	query := fmt.Sprintf(template, ctx.name, sqlset, where)
	params := append(args, ctx.args...)
	var result sql.Result
	result, err = ctx.exec(query, params...)
	if err != nil {
		return
	}
	rowsAffected, err = result.RowsAffected()
	return
}

// 删除
func (ctx *Context) Delete() (rowsAffected int64, err error) {
	template := "delete from %s %s"
	where := sqlwhere(ctx.wheres, Grouping)

	query := fmt.Sprintf(template, ctx.name, where)
	var result sql.Result
	result, err = ctx.exec(query, ctx.args...)
	if err != nil {
		return
	}
	rowsAffected, err = result.RowsAffected()
	return
}

// 查询多条记录，直接使用给定的`sql`和`args`
func (ctx *Context) Select(dest interface{}, sql string, args ...interface{}) error {
	ctx.sql = sql
	ctx.args = args
	return ctx.find(dest, SelectTypeMany)
}

// 查询单条记录，直接使用给定的`sql`和`args`
func (ctx *Context) Get(dest interface{}, sql string, args ...interface{}) error {
	ctx.sql = sql
	ctx.args = args
	return ctx.find(dest, SelectTypeOne)
}

// 直接执行操作，直接使用给定的`sql`和`args`
func (ctx *Context) Exec(sql string, args ...interface{}) (sql.Result, error) {
	return ctx.exec(sql, args...)
}

// 创建表
func (ctx *Context) Create(sql string) (sql.Result, error) {
	return ctx.exec(sql)
}

// 删除表
func (ctx *Context) Drop() (sql.Result, error) {
	return ctx.exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", ctx.name))
}

/////////////////////////private methods//////////////////////

// 重置Context
func (ctx *Context) reset() *Context {
	ctx.sql = ""
	ctx.name = ""
	ctx.what = []string{}
	ctx.wheres = []string{}
	ctx.order = ""
	ctx.group = ""
	ctx.having = ""
	ctx.limit = 0
	ctx.offset = 0
	ctx.args = []interface{}{}
	ctx.tx = nil
	ctx.lockS = false
	ctx.lockX = false
	return ctx
}

// 查询方法
func (ctx *Context) find(dest interface{}, selectType int) (err error) {
	defer ctx.db.pool.Put(ctx)
	ttx, cancel := context.WithTimeout(context.Background(), ctx.db.timeout)
	defer cancel()
	if ctx.sql == "" {
		ctx.sql = ctx.sqlselect(dest)
	}
	switch selectType {
	case SelectTypeOne:
		if ctx.tx != nil {
			err = ctx.tx.GetContext(ttx, dest, ctx.sql, ctx.args...)
		} else {
			err = ctx.db.GetContext(ttx, dest, ctx.sql, ctx.args...)
		}
	case SelectTypeMany:
		if ctx.tx != nil {
			err = ctx.tx.SelectContext(ttx, dest, ctx.sql, ctx.args...)
		} else {
			err = ctx.db.SelectContext(ttx, dest, ctx.sql, ctx.args...)
		}
	default:
		panic("select type err")
	}
	return
}

// update,insert,delete方法
func (ctx *Context) exec(query string, args ...interface{}) (sql.Result, error) {
	log.Printf("littleorm exec sql: <%s>, args: %#v", query, args)
	defer ctx.db.pool.Put(ctx)
	ttx, cancel := context.WithTimeout(context.Background(), ctx.db.timeout)
	defer cancel()

	var ec sqlx.ExecerContext
	if ctx.tx != nil {
		ec = ctx.tx
	} else {
		ec = ctx.db
	}
	return ec.ExecContext(ttx, query, args...)
}

// select查询语句的拼接
func (ctx *Context) sqlselect(dest interface{}) string {
	var sqlArray []string
	sqlArray = append(sqlArray, "select")
	if len(ctx.what) != 0 {
		sqlArray = append(sqlArray, sqljoin(ctx.what, SeqComma))
	} else {
		// 如果不指定字段，取出目标对象的 tag 中的 db 全部填充了，
		// 不使用 * 来填充是因为 sqlx 解析时候如果对象中不包含数据库中全部字段会出现映射错误，会让以后增加数据库字段时候不兼容
		whatFields := decodetags(dest)
		if len(whatFields) > 0 {
			sqlArray = append(sqlArray, sqljoin(whatFields, SeqComma))
		} else {
			sqlArray = append(sqlArray, "*")
		}
	}
	sqlArray = append(sqlArray, "from "+ctx.name)
	if len(ctx.wheres) != 0 {
		sqlArray = append(sqlArray, sqlwhere(ctx.wheres, Grouping))
	}

	if ctx.group != "" {
		sqlArray = append(sqlArray, "group by "+ctx.group)
	}

	if ctx.having != "" {
		sqlArray = append(sqlArray, "having "+ctx.having)
	}

	if ctx.order != "" {
		sqlArray = append(sqlArray, "order by "+ctx.order)
	}

	if ctx.limit != 0 {
		sqlArray = append(sqlArray, fmt.Sprintf("limit %d, %d", ctx.offset, ctx.limit))
	}
	if ctx.lockS {
		sqlArray = append(sqlArray, "lock in share mode")
	}
	if ctx.lockX {
		sqlArray = append(sqlArray, "for update")
	}
	sql := sqljoin(sqlArray, SeqSpace)
	log.Printf("littleorm sql: <%v>, args: %#v", sql, ctx.args)
	return sql
}

///////////////////////////utils method/////////////////////////
// 拼接where条件
func sqlwhere(wheres []string, grouping string) string {
	if len(wheres) > 0 {
		return fmt.Sprintf("where %s", strings.Join(wheres, grouping))
	} else {
		return ""
	}
}

// 拼接数组字符串
func sqljoin(args []string, seq string) string {
	return strings.Join(args, seq)
}

// 解析对象中的 `db tag`
// 参数只能指针，单个对象或者数组，eg: &little, &[]Little
func decodetags(dest interface{}) (fields []string) {
	value := reflect.ValueOf(dest)
	slice := value.Type().Elem()
	var base reflect.Type
	if slice.Kind() == reflect.Slice {
		base = slice.Elem()
	} else {
		base = slice
	}
	for i := 0; i < base.NumField(); i++ {
		dbTag := base.Field(i).Tag.Get(DBTag)
		if dbTag != "" {
			fields = append(fields, dbTag)
		}
	}
	return
}
