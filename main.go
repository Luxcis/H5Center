package main

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileNode 表示文件树中的一个节点
type FileNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"isDir"`
	Size     int64       `json:"size"`
	Children []*FileNode `json:"children,omitempty"`
}

func main() {
	r := gin.Default()
	r.SetFuncMap(template.FuncMap{
		"dict": dict,
	})
	r.LoadHTMLGlob("templates/*")
	r.StaticFS("/h5", http.Dir("./h5"))
	r.GET("/", index)
	r.POST("/api/upload", upload)
	r.DELETE("/api/delete", deleteH5)
	err := r.Run(":8080")
	if err != nil {
		return
	}
}

func index(c *gin.Context) {
	// 扫描h5目录
	rootNode, err := scanDirectory("./h5")
	if err != nil {
		c.HTML(http.StatusInternalServerError, "index.html", gin.H{
			"Title": "文件浏览器 - 错误",
			"Error": err.Error(),
		})
		return
	}
	c.HTML(http.StatusOK, "index.html", gin.H{
		"Title":      "文件浏览器",
		"RootNode":   rootNode,
		"FormatSize": formatFileSize,
	})
}

func upload(c *gin.Context) {
	startTime := time.Now()

	// 从表单中获取文件
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":  0,
			"error": "文件接收失败: " + err.Error(),
		})
		return
	}
	// 获取文件大小
	fileSize := file.Size
	// 获取fileName参数
	fileName := c.PostForm("fileName")
	// 基础保存目录
	baseDir := "./h5"
	// 确保基础目录存在
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":  0,
			"error": "创建基础保存目录失败: " + err.Error(),
		})
		return
	}
	// 处理文件名
	var targetPath string
	var finalFileName string
	if fileName == "" {
		// 如果fileName为空，使用原始文件名
		finalFileName = file.Filename
		targetPath = filepath.Join(baseDir, finalFileName)
	} else {
		// 安全检查：确保fileName不包含路径穿越
		if strings.Contains(fileName, ".") || strings.Contains(fileName, "index") {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":  0,
				"error": "文件名包含不安全的路径",
			})
			return
		}
		// 检查fileName是否包含扩展名
		fileExt := filepath.Ext(fileName)
		if fileExt == "" {
			// 如果fileName没有扩展名，从原始文件名中获取扩展名
			origExt := filepath.Ext(file.Filename)
			// 将扩展名添加到fileName
			fileName = fileName + origExt
		}
		finalFileName = fileName
		targetPath = filepath.Join(baseDir, fileName)
		// 如果fileName包含路径，确保目录存在
		dir := filepath.Dir(targetPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "创建目录结构失败: " + err.Error(),
			})
			return
		}
	}
	// 保存文件
	if err := c.SaveUploadedFile(file, targetPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "保存文件失败: " + err.Error(),
		})
		return
	}
	// 计算处理时间
	processingTime := time.Since(startTime)
	// 记录上传信息
	fmt.Printf("文件已上传: %s (大小: %d bytes, 处理时间: %v)\n", targetPath, fileSize, processingTime)
	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"message":         "文件上传成功",
		"original_name":   file.Filename,
		"saved_name":      finalFileName,
		"path":            targetPath,
		"size":            fileSize,
		"processing_time": processingTime.String(),
	})
}

func deleteH5(c *gin.Context) {
	// 获取要删除的文件路径
	targetPath := c.Query("path")
	if targetPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":  0,
			"error": "文件路径不能为空",
		})
		return
	}

	if !isPathSafe(targetPath) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":  0,
			"error": "非法的文件路径",
		})
		return
	}

	// 构建完整的文件路径
	fullPath := filepath.Join("./h5", targetPath)

	// 检查文件是否存在
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{
				"code":  0,
				"error": "文件不存在",
			})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":  0,
				"error": "获取文件信息失败: " + err.Error(),
			})
		}
		return
	}

	// 执行删除操作
	if fileInfo.IsDir() {
		// 删除目录及其所有内容
		err = os.RemoveAll(fullPath)
	} else {
		// 删除单个文件
		err = os.Remove(fullPath)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":  0,
			"error": "删除文件失败: " + err.Error(),
		})
		return
	}

	// 删除成功
	c.JSON(http.StatusOK, gin.H{
		"code":    1,
		"message": "文件删除成功",
		"path":    targetPath,
	})
}

// 扫描目录，返回文件树
func scanDirectory(root string) (*FileNode, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	rootNode := &FileNode{
		Name:     filepath.Base(root),
		Path:     "", // 根目录路径为空
		IsDir:    info.IsDir(),
		Size:     info.Size(),
		Children: []*FileNode{},
	}
	if !info.IsDir() {
		return rootNode, nil
	}
	// 递归扫描目录
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// 跳过根目录自身
		if path == root {
			return nil
		}
		// 相对路径，用于显示
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// 处理路径分隔符，统一使用/
		relPath = strings.ReplaceAll(relPath, "\\", "/")
		// 创建当前文件节点
		node := &FileNode{
			Name:  filepath.Base(path),
			Path:  relPath,
			IsDir: info.IsDir(),
			Size:  info.Size(),
		}
		// 将节点添加到树中正确的位置
		addNodeToTree(rootNode, node, strings.Split(relPath, "/"))
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 对每个目录的子节点排序，文件夹在前，文件在后
	sortTree(rootNode)
	return rootNode, nil
}

// 将节点添加到树中正确的位置
func addNodeToTree(root *FileNode, node *FileNode, pathParts []string) {
	// 如果路径只有一部分，直接添加到根节点
	if len(pathParts) == 1 {
		if node.IsDir {
			node.Children = []*FileNode{}
		}
		root.Children = append(root.Children, node)
		return
	}
	// 查找当前路径部分对应的子节点
	var dirNode *FileNode
	for _, child := range root.Children {
		if child.Name == pathParts[0] && child.IsDir {
			dirNode = child
			break
		}
	}
	// 如果找不到对应的子节点，创建一个新的目录节点
	if dirNode == nil {
		dirNode = &FileNode{
			Name:     pathParts[0],
			Path:     pathParts[0],
			IsDir:    true,
			Children: []*FileNode{},
		}
		root.Children = append(root.Children, dirNode)
	}
	// 如果只有一层目录且目标是文件，直接添加
	if len(pathParts) == 2 && !node.IsDir {
		node.Name = pathParts[1]
		dirNode.Children = append(dirNode.Children, node)
		return
	}
	// 递归处理剩余的路径部分
	addNodeToTree(dirNode, node, pathParts[1:])
}

// 对文件树排序，目录在前，文件在后，同类型按名称排序
func sortTree(node *FileNode) {
	if !node.IsDir || len(node.Children) == 0 {
		return
	}
	// 对子节点递归排序
	for _, child := range node.Children {
		sortTree(child)
	}
	// 对当前节点的子节点排序
	sort.Slice(node.Children, func(i, j int) bool {
		// 如果一个是目录一个是文件，目录优先
		if node.Children[i].IsDir != node.Children[j].IsDir {
			return node.Children[i].IsDir
		}
		// 否则按名称排序
		return node.Children[i].Name < node.Children[j].Name
	})
}

// 格式化文件大小
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// 将多个键值对组合成map的函数，用于模板
func dict(values ...interface{}) (map[string]interface{}, error) {
	if len(values)%2 != 0 {
		return nil, errors.New("invalid dict call")
	}
	dict := make(map[string]interface{}, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil, errors.New("dict keys must be strings")
		}
		dict[key] = values[i+1]
	}
	return dict, nil
}

// 安全地验证路径是否在h5目录下
func isPathSafe(targetPath string) bool {
	// 安全检查：防止目录穿越
	if strings.Contains(targetPath, "..") || strings.HasPrefix(targetPath, "/") || strings.HasPrefix(targetPath, "\\") {
		return false
	}
	// 构建完整的文件路径
	fullPath := filepath.Join("./h5", targetPath)
	// 确保路径在h5目录下（规范化路径后再次检查）
	absBasePath, _ := filepath.Abs("./h5")
	absFilePath, err := filepath.Abs(fullPath)
	if err != nil {
		return false
	}
	// 确保目标文件在h5目录下
	if !strings.HasPrefix(absFilePath, absBasePath) {
		return false
	}
	return true
}
