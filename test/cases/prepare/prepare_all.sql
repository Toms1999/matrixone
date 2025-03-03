-- @suit

-- @case
-- @desc:Test prepared statements with signed and unsigned integer user variables
-- @label:bvt

drop table if exists numbers;
CREATE TABLE numbers
(pk INTEGER PRIMARY KEY,
 ui BIGINT UNSIGNED,
 si BIGINT
);

INSERT INTO numbers VALUES
(0, 0, -9223372036854775808), (1, 18446744073709551615, 9223372036854775807);


-- @bvt:issue#4491
SET @ui_min = CAST(0 AS UNSIGNED);
-- @bvt:issue

SET @ui_min = 0;
SET @ui_max = 18446744073709551615;
SET @si_min = -9223372036854775808;
SET @si_max = 9223372036854775807;

-- @bvt:issue#4482
PREPARE s1 FROM 'SELECT * FROM numbers WHERE ui=?';
EXECUTE s1 USING @ui_min;
EXECUTE s1 USING @ui_max;
EXECUTE s1 USING @si_min;
EXECUTE s1 USING @si_max;
DEALLOCATE PREPARE s1;
-- @bvt:issue

PREPARE s2 FROM 'SELECT * FROM numbers WHERE si=?';
EXECUTE s2 USING @ui_min;
EXECUTE s2 USING @ui_max;
EXECUTE s2 USING @si_min;
EXECUTE s2 USING @si_max;

DEALLOCATE PREPARE s2;

DROP TABLE numbers;


-- @case
-- @desc:Test prepared statements with float and double floating user variables
-- @label:bvt
drop table if exists test_table;
CREATE TABLE test_table
(pk INTEGER PRIMARY KEY,
 fl FLOAT,
 dou DOUBLE
);

-- @bvt:issue#4484
set @float1_num=1.2345678;
set @float2_num=1.8765432;
set @double_num1=1.223344556677889900;
set @double_num2=1.223344556677889900;
INSERT INTO test_table VALUES(0, @float1_num, @double_num1), (1, @float2_num, @double_num2);
-- @bvt:issue


-- @bvt:issue#4487
INSERT INTO test_table VALUES(0, 1.2345678, 1.223344556677889900), (1, 1.876599999432, 1.223344556677889900);
select * from test_table;
select * from test_table where fl=1.2345678;

SET @fl_hit=1.2345678;
SET @fl_not_hit=1.234567800;
SET @dou_not_hit=1.223344556677889;
SET @dou_hit=1.223344556677889900;

PREPARE s1 FROM 'SELECT * FROM test_table WHERE fl=?';
PREPARE s2 FROM 'SELECT * FROM test_table WHERE dou=?';

EXECUTE s1 USING @fl_hit;
EXECUTE s1 USING @fl_not_hit;
EXECUTE s1 USING @dou_hit;
EXECUTE s1 USING @dou_not_hit;
EXECUTE s2 USING @fl_hit;
EXECUTE s2 USING @fl_not_hit;
EXECUTE s2 USING @dou_hit;
EXECUTE s2 USING @dou_not_hit;

DEALLOCATE PREPARE s1;
DEALLOCATE PREPARE s2;
-- @bvt:issue

DROP TABLE test_table;


-- @case
-- @desc:Test prepared statements with varchar and char string user variables
-- @label:bvt
drop table if exists t1;
create table t1 (
    str1 varchar(25),
    str2 char(25)
);

insert into t1 values('a1','b1'),('a2', 'b2'),('a3', '');
insert into t1(str1) values('a4');

prepare s1 from 'update t1 set str1="xx1" where str2=?';

set @hit_str2='b1';
set @not_hit_str2='b';

execute s1 using @hit_str2;
execute s1 using @not_hit_str2;

select * from t1;

DEALLOCATE PREPARE s1;


prepare s2 from 'update t1 set str2="yy1" where str1=?';

set @hit_str1='a2';
set @not_hit_str2='a';

execute s2 using @hit_str1;
execute s2 using @not_hit_str1;

select * from t1;

DEALLOCATE PREPARE s2;


-- @bvt:issue#4526
prepare s3 from 'select * from t1 where str1 like ?';
prepare s4 from 'select * from t1 where str2 not like ?';

set @a='a%';
execute s3 using @a;

DEALLOCATE PREPARE s3;
DEALLOCATE PREPARE s4;
-- @bvt:issue

prepare s5 from 'select * from t1 where str2=?';

set @hit_empty='';

execute s5 using @hit_empty;

DEALLOCATE PREPARE s5;

DROP TABLE t1;


-- @case
-- @desc:Test prepared statements with DATE and DATETIME and TIMESTAMP time user variables
-- @label:bvt

drop table if exists t2;
create table t2 (
    time1 Date,
    time2 DateTime,
    time3 TIMESTAMP
);

-- @bvt:issue#4510
insert into t2 values ('1000-01-01', '0001-01-01 00:00:00.000000', '2038-01-19 03:14:07.999999');
insert into t2 values ('1000-01-01', '9999-12-31 23:59:59.999999', '2038-01-19 03:14:07.999999');
insert into t2 values ('9999-12-31', '9999-12-31 23:59:59.999999', '2038-01-19 03:14:07.999999');
-- @bvt:issue

-- @bvt:issue#3703
insert into t2 values ('1000-01-01', '0001-01-01 00:00:00.000000', '1970-01-01 00:00:01.000000');
insert into t2 values ('1000-01-01', '0001-01-01 00:00:00.000000', '1970-01-01 00:00:01.000000');
insert into t2 values ('1000-01-01', '0001-01-01 00:00:00.000000', '1970-01-01 00:00:01.000000');
-- @bvt:issue

insert into t2 values ('2022-10-24', '2022-10-24 10:10:10.000000', '2022-10-24 00:00:01.000000');
insert into t2 values ('2022-10-25', '2022-10-25 10:10:10.000000', '2022-10-25 00:00:01.000000');
insert into t2 values ('2022-10-26', '2022-10-26 10:10:10.000000', '2022-10-26 00:00:01.000000');

-- @bvt:issue#4510
select * from t2;
-- @bvt:issue

set @max_date='9999-12-31';
set @min_date='1000-01-01';

set @max_datetime='9999-12-31 23:59:59.999999';
set @min_datetime='0001-01-01 00:00:00.000000';
set @max_timestamp='1970-01-01 00:00:01.000000';
set @min_timestamp='2038-01-19 03:14:07.999999';


prepare s1 from 'select * from t2 where time1=?';

execute s1 using @max_date;
-- @bvt:issue#4604
execute s1 using @min_date;
-- @bvt:issue
execute s1 using @max_datetime;
execute s1 using @min_datetime;
execute s1 using @max_timestamp;
execute s1 using @min_timestamp;

DEALLOCATE PREPARE s1;


prepare s2 from 'select * from t2 where time2=?';

execute s2 using @max_date;
execute s2 using @min_date;
execute s2 using @max_datetime;
-- @bvt:issue#4604
execute s2 using @min_datetime;
-- @bvt:issue
execute s2 using @max_timestamp;
execute s2 using @min_timestamp;

DEALLOCATE PREPARE s2;


prepare s3 from 'select * from t2 where time3=?';

-- @bvt:issue#4527
execute s3 using @max_date;
execute s3 using @min_date;
execute s3 using @max_datetime;
execute s3 using @min_datetime;
execute s3 using @max_timestamp;
execute s3 using @min_timestamp;
-- @bvt:issue

DEALLOCATE PREPARE s3;


set @time1='2022-10-24';
set @time2='2022-10-25 10:10:10.000000';
set @time3='2022-10-26 00:00:01.000000';

prepare s4 from 'delete from t2 where time1=?';
prepare s5 from 'delete from t2 where time2=?';
prepare s6 from 'delete from t2 where time3=?';

execute s4 using @time1;
execute s5 using @time2;
execute s6 using @time3;

-- @bvt:issue#4510
select * from t2;
-- @bvt:issue

DEALLOCATE PREPARE s4;
DEALLOCATE PREPARE s5;
DEALLOCATE PREPARE s6;

drop table t2;


-- @case
-- @desc:Test prepared statements with decimal64 and decimal128 decimal variables
-- @label:bvt

drop table if exists t3;
create table t3(
    dec1 decimal(5,2) default  NULL,
    dec2 decimal(25,10)
);

insert into t3 values (12.345, 10000.222223333344444);
insert into t3 values (123.45, 1111122222.222223333344444);
insert into t3 values (133.45, 1111122222.222223333344444);
insert into t3 values (153.45, 1111122222.222223333344444);
insert into t3 values (123.45678, 111112222233333.222223333344444);
insert into t3(dec2) values (111112222233333.222223333344444);

select * from t3;

set @hit_dec1=12.34;
set @hit_dec2=1111122222.2222233333;
set @dec1_max=200;
set @dec1_min=10;
set @dec2_max=111112222233339;
set @dec2_min=1000;

prepare s1 from 'select * from t3 where dec1>?';
prepare s2 from 'select * from t3 where dec1>=?';
prepare s3 from 'select * from t3 where dec1<?';
prepare s4 from 'select * from t3 where dec1<=?';
prepare s5 from 'select * from t3 where dec1<>?';
prepare s6 from 'select * from t3 where dec1!=?';
prepare s7 from 'select * from t3 where dec1 between ? and ?';
prepare s8 from 'select * from t3 where dec1 not between ? and ?';

-- @bvt:issue#4604
execute s1 using @hit_dec1;
-- @bvt:issue
execute s1 using @dec1_max;
execute s1 using @dec1_min;

execute s2 using @hit_dec1;
execute s2 using @dec1_max;
execute s2 using @dec1_min;

execute s3 using @hit_dec1;
execute s3 using @dec1_max;
execute s3 using @dec1_min;
-- @bvt:issue#4604
execute s4 using @hit_dec1;
-- @bvt:issue
execute s4 using @dec1_max;
execute s4 using @dec1_min;
-- @bvt:issue#4604
execute s5 using @hit_dec1;
-- @bvt:issue
execute s5 using @dec1_max;
execute s5 using @dec1_min;
-- @bvt:issue#4604
execute s6 using @hit_dec1;
-- @bvt:issue
execute s6 using @dec1_max;
execute s6 using @dec1_min;


execute s7 using @dec1_min, @dec1_max;
execute s7 using @dec1_max, @dec1_min;

execute s8 using @dec1_min, @dec1_max;
execute s8 using @dec1_max, @dec1_min;

DEALLOCATE PREPARE s1;
DEALLOCATE PREPARE s2;
DEALLOCATE PREPARE s3;
DEALLOCATE PREPARE s4;
DEALLOCATE PREPARE s5;
DEALLOCATE PREPARE s6;
DEALLOCATE PREPARE s7;
DEALLOCATE PREPARE s8;

drop table t3;


-- @case
-- @desc:test group by having scene
-- @label:bvt
drop table if exists t4;
create table t4(
    a1 INT,
    str1 varchar(25)
);

insert into t4 values (10, 'aaa');
insert into t4 values (10, 'bbb');
insert into t4 values (20, 'aaa');
insert into t4 values (20, 'bbb');
insert into t4 values (20, 'bbb');
insert into t4 values (20, 'bbb');
insert into t4 values (20, 'bbb');
insert into t4 values (20, 'ccc');

set @min=1;
set @max=5;

prepare s1 from 'select str1,count(a1) as c from t4 group by str1 having count(a1)>?';
prepare s2 from 'select str1,count(a1) as c from t4 group by str1 having count(a1)>=?';
prepare s3 from 'select str1,count(a1) as c from t4 group by str1 having count(a1)<?';
prepare s4 from 'select str1,count(a1) as c from t4 group by str1 having count(a1)<=?';

execute s1 using @min;
execute s2 using @min;
execute s3 using @max;
execute s4 using @max;


DEALLOCATE PREPARE s1;
DEALLOCATE PREPARE s2;
DEALLOCATE PREPARE s3;
DEALLOCATE PREPARE s4;

drop table t4;


-- @case
-- @desc:test join on where scene
-- @label:bvt

drop table if exists t5;
create table t5(
    a1 int,
    a2 varchar(25)
);

drop table if exists t6;
create table t6(
    b1 int,
    b2 varchar(25)
);

insert into t5 values (10, 'xxx1');
insert into t5 values (20, 'xxx1');
insert into t5 values (30, 'xxx1');
insert into t5 values (10, 'yyy1');
insert into t5 values (10, 'zzz1');
insert into t5 values (20, 'yyy1');
insert into t5 values (40, 'xxx1');

insert into t6 values (10, 'aaa1');
insert into t6 values (20, 'aaa1');
insert into t6 values (30, 'aaa1');
insert into t6 values (40, 'bbb1');
insert into t6 values (50, 'aaa1');
insert into t6 values (60, 'ccc1');
insert into t6 values (10, 'aaa1');
insert into t6 values (20, 'ccc1');

set @a2_val='yyy1';
set @min=10;

prepare s1 from 'select * from t5 a inner join t6 b on a.a1=b.b1 where a.a2=?';
prepare s2 from 'select * from t5 a inner join t6 b on a.a1=b.b1 where a.a1>=?';
prepare s3 from 'select * from t5 a inner join t6 b on a.a1=b.b1 where b.b1>=?';

prepare s4 from 'select * from t5 a left join t6 b on a.a1=b.b1 where a.a2=?';
prepare s5 from 'select * from t5 a left join t6 b on a.a1=b.b1 where a.a1>=?';
prepare s6 from 'select * from t5 a left join t6 b on a.a1=b.b1 where b.b1>=?';

prepare s7 from 'select * from t5 a right join t6 b on a.a1=b.b1 where a.a2=?';
prepare s8 from 'select * from t5 a right join t6 b on a.a1=b.b1 where a.a1>=?';
prepare s9 from 'select * from t5 a right join t6 b on a.a1=b.b1 where b.b1>=?';

execute s1 using @a2_val;
execute s2 using @min;
execute s3 using @min;

execute s4 using @a2_val;
execute s5 using @min;
execute s6 using @min;

execute s7 using @a2_val;

execute s8 using @min;
execute s9 using @min;

DEALLOCATE PREPARE s1;
DEALLOCATE PREPARE s2;
DEALLOCATE PREPARE s3;
DEALLOCATE PREPARE s4;
DEALLOCATE PREPARE s5;
DEALLOCATE PREPARE s6;
DEALLOCATE PREPARE s7;
DEALLOCATE PREPARE s8;
DEALLOCATE PREPARE s9;

set @a1=10;
set @b1=10;

prepare s1 from 'select * from t5 where a1 > ? union select * from t6 where b1 > ?';
prepare s2 from 'select * from t5 where a1 > ? union all select * from t6 where b1 > ?';

execute s1 using @a1, @b1;
execute s2 using @a1, @b1;


drop table t5;
drop table t6;

-- @case
-- @desc:test maxint operation
-- @label:bvt

set @maxint=18446744073709551615;
select @maxint;

SELECT @maxint + 0e0;
SELECT 18446744073709551615 + 0e0;

SELECT @maxint + 0.0;
SELECT 18446744073709551615 + 0.0;


PREPARE s FROM 'SELECT 0e0 + ?';

EXECUTE s USING @maxint;
DEALLOCATE PREPARE s;

PREPARE s FROM 'SELECT 0.0 + ?';

EXECUTE s USING @maxint;
DEALLOCATE PREPARE s;

PREPARE s FROM 'SELECT 0 + ?';

EXECUTE s USING @maxint;
DEALLOCATE PREPARE s;

PREPARE s FROM 'SELECT concat(?,"")';

EXECUTE s USING @maxint;
DEALLOCATE PREPARE s;
