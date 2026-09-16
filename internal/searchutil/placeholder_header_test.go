package searchutil

import "testing"

func TestIsPlaceholderHeaderKV(t *testing.T) {
	stats := "col_3: 信息系统集成高级管理,col_4: 1.0\ncol_3: 全国软件行业人才证书,col_4: 1.0"
	unnamed := "Unnamed: 2: 系统集成项目管理师,Unnamed: 3: 1"
	person := "序号: 67.0,工号: GDJL16616,姓名: 夏雨欣,专业证书: 系统集成项目管理师"
	certOnly := "；软件测评师,专业证书: 1：系统集成项目管理师，编号：1,岗位: 软件测试主管工程师"

	if !IsPlaceholderHeaderKV(stats) {
		t.Fatal("expected col_N stats row to be placeholder")
	}
	if !IsPlaceholderHeaderKV(unnamed) {
		t.Fatal("expected Unnamed stats row to be placeholder")
	}
	if IsPlaceholderHeaderKV(person) {
		t.Fatal("person record must not be treated as placeholder")
	}
	if IsPlaceholderHeaderKV(certOnly) {
		t.Fatal("named-field certificate fragment must not be treated as placeholder")
	}
	if IsPlaceholderHeaderKV("short") || IsPlaceholderHeaderKV("") {
		t.Fatal("non-KV content must not match")
	}
}
