# littleorm

一个轻量级的`golang`数据库查询库，基于`sqlx`封装了一层，提供了一些更加便捷的拼接`sql`的方法

## 安装

```sh
> go get github.com/lujin123/littleorm
```

## 使用

### 常用的拼接 SQL 的方法

- **Name**: 指定数据库表名
- **What**: 指定查询返回的字段，参数是一个字符字符串数组(`[]string`)
- **Where**: 指定查询、更新、删除操作的条件，参数是字符串和对应的参数
- **WhereIn**: 指定条件中的`in`操作，可以方便的将数组映射进去
- **Order**: 指定查询排序
- **Limit**: 指定返回的条数
- **Offset**: 指定偏移量
- **Group**: 指定分组
- **Having**: 指定分组过滤条件和参数
- **LockX**: 指定使用互斥锁（`for update`）
- **LockS**: 指定使用共享锁（`lock in share mode`）

### 查询单条记录

```golang
var (
    little LittleOrm
    err    error
)
err = db.Acquire().Name("little_orm").What([]string{"id", "name", "age"}).Where("id=?", 1).FindOne(&little)
...
```

### 查询多条记录

```golang
var (
    littles []LittleOrm
    err     error
)
err = db.Acquire().Name("little_orm").FindMany(&littles)
...
```

### 插入记录

```golang
data := map[string]interface{}{
    "name": "allen",
    "age":  18,
}
result, err := db.Acquire().Name("little_orm").Insert(data)
...
```

### 批量插入记录

```golang
fields := []string{"name", "age"}
data := [][]interface{}{
    {"allen2", 19},
    {"allen3", 10},
}
result, err := db.Acquire().Name("little_orm").InsertBatch(fields, data...)
```

### 更新记录

```golang
rows, err := db.Acquire().Name("little_orm").Where("id=?", 2).Update("name=?, age=age+?", "allen4", 2)
```

### 使用 `map` 更新记录

```golang
data := map[string]interface{}{
    "name": "allen5",
    "age":  10,
}
rows, err := db.Acquire().Name("little_orm").Where("id=?", 2).UpdateMap(data)
```

### 删除记录

```golang
rows, err := db.Acquire().Name("little_orm").Where("id=?", 3).Delete()
```

### 带有 `in` 操作的条件

```golang
var (
    littles []LittleOrm
    err     error
)
err = db.Acquire().Name("little_orm").WhereIn("id", []interface{}{1, 2}).FindMany(&littles)
```

**注意**：`WhereIn`中的参数数组必须是`[]interface{}`类型，否则传入参数会报错

### 关于事务

用`db.Acquire()`获取到的都是不带没有开启事务的连接，如果需要开启事务，需要使用`db.AcquireTx(tx)`方法获取，需要提前开启事务操作，获取到`tx`变量

为了方便事务的管理，实现了一个`WithTx`方法，可以方便的管理事务周期，只需要传入一个方法处理业务逻辑以及该方法的参数，这个传入的方法签名必须是 `func (tx *sqlx.Tx, args interface{}) error`，方法中的`tx`会被自动注入，但是参数和返回值有限制，都是单参数，所以在需要传入多个参数的时候，需要组合成一个`struct`再传入

使用方法如下：

```golang
err := db.WithTx(updateAge, 100)

func updateAge(tx *sqlx.Tx, age interface{}) error {
    var (
        little LittleOrm
    )
    err := db.AcquireTx(tx).Name("little_orm").Where("id=?", 1).LockX().FindOne(&little)
    if err != nil {
        return err
    }

    rows, err := db.AcquireTx(tx).Name("little_orm").Where("id=?", little.Id).Update("age=age+?", age)
    if err != nil {
        return err
    }
    if rows != 1 {
        return errors.New("update row affect error")
    }
    return nil
}
```

如果不方便就自己去管理事务吧...

### 更多

还提供了几个直接执行`sql`的方法：

- **Select**
- **Get**
- **Exec**

更多的使用方法尅在`littleorm_test.go`文件中查看

## 最后

这个是在项目中直接使用`sqlx`的一些总结，做一些封装能写起来更方便，当然性能也会有损失...
