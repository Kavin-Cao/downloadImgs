package main

import (
	"flag"
	"github.com/astaxie/beego/logs"
	"github.com/astaxie/beego/orm"
	"github.com/emirpasic/gods/sets/hashset"
	_ "github.com/go-sql-driver/mysql"
	"io/ioutil"
	"os"
	"strings"
	"path/filepath"
	"net/http"
	"fmt"
	"github.com/jianfengye/image-sign/src/signer"
	"image/jpeg"
)

type AccessManagment struct {
	AccessNum    string
	QrcodeUrl    string
	AccessName   string
	BuildingName string
	UnitName     string
	Building     string
	Unit         string
}

var dir string

var files map[string]string

func main() {
	f := flag.String("f", "", "要生成门禁编号的小区编号文本文件路径,多个用空白符(空格或者回车)分隔")
	c := flag.String("c", "", "要生成门禁编号的小区编号,多个用空格分隔,并使用英文引号包含起来")
	flag.Parse()
	communityNums := hashset.New()

	if *f != "" {
		logs.Info("解析", *f)
		fileInfo, err := os.Stat(*f)
		if err != nil {
			logs.Error("打开文件", f, "失败", err)
			os.Exit(-1)
		}
		if fileInfo.IsDir() {
			logs.Error("文件", f, "是一个文件夹")
			os.Exit(-1)
		}
		fileByte, err := ioutil.ReadFile(*f)
		if err != nil {
			logs.Error("打开文件", f, "失败", err)
			os.Exit(-1)
		}
		fstr := string(fileByte)
		communties := strings.Split(fstr, "\\s+")
		for i := 0; i < len(communties); i++ {
			community := communties[i]
			community = strings.TrimSpace(community)
			communityNums.Add(community)
		}
	}

	if *c != "" {
		logs.Info("解析", *c)
		communties := strings.Split(*c, "\\s+")
		for i := 0; i < len(communties); i++ {
			community := communties[i]
			community = strings.TrimSpace(community)
			communityNums.Add(community)
		}
	}
	if communityNums.Size() == 0 {
		flag.PrintDefaults()
		return
	} else {
		logs.Info("小区:", communityNums.Values())
	}
	orm.RegisterDriver("mysql", orm.DRMySQL)
	err := orm.RegisterDataBase("default", "mysql", `root:root.@tcp(127.0.0.1:3306)/ecctest?charset=utf8`)
	if err != nil {
		logs.Error("注册数据库实例错误:", err)
	}

	orm.Debug = true
	o := orm.NewOrm()
	o.Using("default")

	rs := o.Raw(`select a.access_num as accessNum,a.qrcode_url as qrcodeUrl,
			a.access_name as accessName,a.building_name as buildingName,
		 	a.unit_name as unitName,a.building as building,a.unit as unit,
		 	b.community_name as communityName
		     from
		 	access_management a left join community b on a.community_number = b.community_number
		where a.community_number in(?)`, communityNums.Values())
	var accessManagments []orm.Params
	logs.Info("分隔线*********************************************************************************************")
	rows2, err2 := rs.Values(&accessManagments)
	if err2 != nil {
		logs.Error("查询小区门禁信息错误:", err2)
	}
	logs.Info("查询到门禁信息---->", rows2)
	pwd,_ := os.Getwd()
	separator := string(filepath.Separator)
	dir = pwd + separator + "门禁二维码" + separator
	err = os.MkdirAll(dir,os.ModePerm)
	if err != nil {
		logs.Error(err)
	}
	logs.Info("二维码图片(.png)将保存到"+dir)
	var done chan int = make(chan int,len(accessManagments))
	files = make(map[string]string)
	for _,accessManagment := range accessManagments {
		var communityName,accessNum,accessName,qrcodeUrl,buildingName,unitName string
		var building,unit int
		if val,ok := accessManagment["communityName"].(string);ok {
			communityName = val
		}
		if val,ok := accessManagment["accessNum"].(string);ok {
			accessNum = val
		}
		if val,ok := accessManagment["accessName"].(string);ok {
			accessName = val
		}
		if val,ok := accessManagment["qrcodeUrl"].(string);ok {
			qrcodeUrl = val
		}
		if val,ok := accessManagment["buildingName"].(string);ok {
			buildingName = val
		}
		if val,ok := accessManagment["unitName"].(string);ok {
			unitName = val
		}
		if val,ok := accessManagment["building"].(int);ok {
			building = val
		}
		if val,ok := accessManagment["unit"].(int);ok {
			unit = val
		}
		go loadPNG(&done,communityName,accessNum,accessName,qrcodeUrl,buildingName,unitName,building,unit)
	}
	for i:=0;i<len(accessManagments);i++ {
		<- done
	}
	logs.Info("下载完成!")
	logs.Info("门禁编号水印处理......")
	for accessNum,filename := range files{
		logs.Info("开始处理",filename)
		watermark(filename,accessNum)
	}
}

func loadPNG(done *chan int,communityName,accessNum,accessName,qrcodeUrl,buildingName,unitName string,building,unit int){
	if len(strings.TrimSpace(buildingName)) == 0 && building > 0{
		buildingName = fmt.Sprintf("%d栋", building)
	}
	if len(strings.TrimSpace(unitName)) == 0 && unit > 0 {
		unitName = fmt.Sprintf("%d单元", unit)
	}
	var filename string
	if len(buildingName + unitName + "") == 0{
		if len(accessName+"") == 0{
			filename = dir + communityName + accessNum + ".jpg"
		}else {
			filename = dir + communityName + accessName + "-" + accessNum + ".jpg"
		}
	}else{
		filename = dir + communityName + buildingName + unitName + "-" + accessNum + ".jpg"
	}
	downloadPNG(done,qrcodeUrl,accessNum,filename)
}

func downloadPNG(done *chan int,qrcodeUrl,accessNum,filename string){
	qrcodeUrl = "http://136.96.32.23:100/" + qrcodeUrl
	logs.Info(qrcodeUrl)
	resp,err := http.Get(qrcodeUrl)
	if err != nil {
		logs.Error(err)
		return
	}
	body := resp.Body
	defer body.Close()
	defer func() {
		*done <- 1
	}()

	file,err := os.OpenFile(filename,os.O_RDWR,os.ModePerm)
	if os.IsNotExist(err){
		file,err = os.Create(filename)
	}
	defer file.Close()
	if err != nil {
		logs.Error(err)
		return
	}
	img,err := jpeg.Decode(body)
	if err != nil {
		logs.Error(err)
		return
	}
	err = jpeg.Encode(file,img,nil)
	if err != nil {
		logs.Error(err)
		return
	}else{
		files[accessNum] = filename
		fileInfo,_ := file.Stat()
		logs.Info(filename," size", fileInfo.Sys(),"下载成功")
	}
}

func watermark(filename,accessNum string) (err error){
	fileFrom,err := os.OpenFile(filename,os.O_RDWR,os.ModePerm)
	if(err != nil){
		return err
	}
	base := filepath.Base(filename)
	ext := filepath.Ext(base)
	toFilename := dir + strings.TrimRight(base,ext) + "-watermark" + ext
	fileTo,err := os.OpenFile(toFilename,os.O_RDWR,os.ModePerm)
	if(err != nil){
		fileTo,err = os.Create(toFilename)
	}
	defer fileFrom.Close()
	defer fileTo.Close()
	signWriter := signer.NewSigner("font/luximr.ttf")
	img,err := jpeg.Decode(fileFrom)
	if(err != nil){
		logs.Info(err)
		return
	}
	rect := img.Bounds()
	width := rect.Dy()
	height := rect.Dy()
	logs.Info(filename,"width=",width,"height=",height)
	signWriter.SetStartPoint(290, 290)
	signWriter.SetSignPoint(200, 200)
	err = signWriter.Sign(fileFrom, fileTo, "                            ", 1)
	if err != nil {
		logs.Error(err)
		return err
	}
	return nil
}
