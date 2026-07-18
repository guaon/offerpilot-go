package knowledge

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"regexp"
	"strings"
)

type KnowledgeSearch struct {
	db                *sql.DB
	embeddingProvider EmbeddingProvider
}

func NewKnowledgeSearch(db *sql.DB, provider EmbeddingProvider) *KnowledgeSearch {
	return &KnowledgeSearch{
		db:                db,
		embeddingProvider: provider,
	}
}

func CreateKnowledgeTables(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS knowledge(
		   id TEXT PRIMARY KEY,
		   title TEXT NOT NULL,
		   dimension TEXT NOT NULL,
		   content TEXT NOT NULL,
		   source_file TEXT,
		   question TEXT,
		   expert_answer TEXT,
		   novice_answer TEXT,
		   tags TEXT
		);`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts USING fts5(id,title,content,dimension,question);`,
		`CREATE TABLE IF NOT EXISTS embedding(
		    knowledge_id TEXT NOT NULL PRIMARY KEY,
			vector BLOB NOT NULL,
			model TEXT NOT NULL,
			create_at INTEGER NOT NULL
		);`,
		`CREATE TRIGGER IF NOT EXISTS knowledge_fts_sync AFTER INSERT ON knowledge BEGIN
		    INSERT INTO knowledge_fts(rowid,id,title,content,dimension,question)
			VALUES (new.rowid,new.id,new.title,new.content,new.dimension,new.question);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS knowledge_fts_update AFTER UPDATE ON knowledge BEGIN
		    UPDATE knowledge_fts SET id=new.id,title=new.title,content=new.content,
			                         dimension=new.dimension,question=new.question WHERE rowid=old.rowid
		END;`,
		`CREATE TRIGGER IF NOT EXISTS knowledge_fts_delete AFTER DELETE ON knowledge BEGIN
			DELETE FROM knowledge_fts WHERE rowid = old.rowid;
		END;`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to execute query:%w", err)
		}

	}
	return nil
}

func (ks *KnowledgeSearch) Search(opts SearchOptions) ([]*SearchResult, error) {
	limit := opts.Limit
	if limit == 0 {
		limit = 5
	}

	method := opts.Method
	if method == "" {
		method = "hybrid"
	}

	if method == "fts" {
		return ks.ftsSearch(opts.Query, opts.Dimension, limit)

	}
	if method == "embedding" {
		return ks.embeddingSearch(opts.Query, opts.Dimension, limit)
	}
	ftsResults, err := ks.ftsSearch(opts.Query, opts.Dimension, limit)
	if err != nil {
		return nil, err
	}

	embResults, err := ks.embeddingSearch(opts.Query, opts.Dimension, limit)
	if err != nil {
		return nil, err
	}
	return ks.mergeResults(ftsResults, embResults, limit), nil

}

//直接通过关键词查找
func (ks *KnowledgeSearch) ftsSearch(query, dimension string, limit int) ([]*SearchResult, error) {
	cleanQuery := regexp.MustCompile(`[^\w一-鿿\s]`).ReplaceAllString(query, " ") //将无效字符变为空格
	terms := strings.Fields(cleanQuery)                                         //按空白字符分割字符串，返回单词切片。
	if len(terms) == 0 {
		return nil, nil
	}

	var ftsQuery string
	for i, term := range terms {
		if i > 0 {
			ftsQuery += " OR "
		}
		ftsQuery += "\"" + term + "\""
	}

	sql := `
	     SELECT k.*,rank
		 FROM knowledge_fts f
		 JOIN knowledge k ON k.rowid=f.rowid
		 WHERE knowledge_fts MATCH ?
	
	`

	params := []interface{}{ftsQuery}

	if dimension != "" {
		sql += ` AND k.dimension= ?`
		params = append(params, dimension)
	}

	sql += ` ORDER BY rank LIMIT ?`
	params = append(params, limit)

	rows, err := ks.db.Query(sql, params...)
	if err != nil {
		return nil, fmt.Errorf("fts query failed:%w", err)
	}
	defer rows.Close()

	var results []*SearchResult
	for rows.Next() {
		var entry KnowledgeEntry
		var rank float64
		if err := ks.scanKnowledgeRow(rows, &entry); err != nil {
			return nil, fmt.Errorf("scan row failed:%w", err)
		}
		if err := rows.Scan(&rank); err != nil {
			return nil, fmt.Errorf("scan rank failed:%w", err)
		}
		results = append(results, &SearchResult{
			Entry:     &entry,
			Score:     math.Abs(rank),
			MatchType: MatchTypeFTS,
		})
	}

	return results, nil

}


//通过向量搜索
func (ks *KnowledgeSearch) embeddingSearch(query, dimension string, limit int) ([]*SearchResult, error) {
	if ks.embeddingProvider == nil {
		return ks.likeFallback(query, dimension, limit)
	}

	queryVector, err := ks.embeddingProvider.Embed(query)
	if err != nil {
		return ks.likeFallback(query, dimension, limit)
	}

	sql := `
	    SELECT e.vector,k.*
		FROM embeddings e
		JOIN knowledge k ON k.id=e.knowledge_id
	`

	params := []interface{}{}

	if dimension != "" {
		sql += ` WHERE k.dimension = ?`
		params = append(params, dimension)
	}

	rows, err := ks.db.Query(sql, params...)

	if err != nil {
		return ks.likeFallback(query, dimension, limit)
	}
	defer rows.Close()

	var scored []struct {
		entry      KnowledgeEntry
		similarity float64
	}

	for rows.Next() {
		var vector []byte
		var entry KnowledgeEntry
		if err := rows.Scan(&vector, &entry.ID, &entry.Title, &entry.Dimension, &entry.Content,
			&entry.SourceFile, &entry.Question, &entry.ExpertAnswer, &entry.NoviceAnswer, &entry.Tags); err != nil {
			continue
		}
		storedVector := ks.deserializeVector(vector)
		similarity := ks.cosineSimilarity(queryVector, storedVector)
		scored = append(scored, struct {
			entry      KnowledgeEntry
			similarity float64
		}{entry, similarity})

	}

	if len(scored) == 0 {
		return ks.likeFallback(query, dimension, limit)
	}

	for i := 0; i < len(scored)-1; i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].similarity > scored[i].similarity {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	var results []*SearchResult
	for _, item := range scored[:min(len(scored), limit)] {
		results = append(results, &SearchResult{
			Entry:     &item.entry,
			Score:     item.similarity,
			MatchType: MatchTypeEmbedding,
		})
	}

	return results, nil

}

//模糊查找
func (ks *KnowledgeSearch) likeFallback(query, dimension string, limit int) ([]*SearchResult, error) {
	searchTerm := query
	if len(searchTerm) > 20 {
		searchTerm = searchTerm[:20]
	}

	sql := `SELECT * FROM knowledge WHERE content LIKE ?`
	params := []interface{}{"%" + searchTerm + "%"}

	if dimension != "" {
		sql += ` AND dimension = ?`
		params = append(params, dimension)

	}

	sql += ` LIMIT ? `
	params = append(params, limit)

	rows, err := ks.db.Query(sql, params...)
	if err != nil {
		return nil, fmt.Errorf("like fallback query failed:%w", err)
	}
	defer rows.Close()

	var results []*SearchResult
	var i int
	for rows.Next() {
		var entry KnowledgeEntry
		if err := ks.scanKnowledgeRow(rows, &entry); err != nil {
			return nil, fmt.Errorf("scan row failed:%w", err)
		}

		results = append(results, &SearchResult{
			Entry:     &entry,
			Score:     1 - float64(i)*0.1,
			MatchType: MatchTypeEmbedding,
		})

		i++

	}
	return results, rows.Err()
}

//知识条目生成向量嵌入并存储到数据库
func (ks *KnowledgeSearch)IndexEmbeddings(ids []string)(int,error){
	if ks.embeddingProvider==nil{
		return 0,fmt.Errorf("no embedding provider configured")

	}


	//查询待索引的知识
	var sql string
	var params []interface{}

	if len(ids)>0{
		placeholders:=make([]string,len(ids))
		for i:=range placeholders{
			placeholders[i]="?"
		}
		sql=`SELECT * FROM knowledge WHERE id IN(`+strings.Join(placeholders,",")+`)`
		params=make([]interface{},len(ids))
		for i,id:=range ids{
			params[i]=id
		}

	}else{
		sql=`SELECT * FROM knowledge WHERE id NOT IN (SELECT knowledge_id FROM embeddings)`
	}

	rows,err:=ks.db.Query(sql,params...)

	if err!=nil{
		return 0,fmt.Errorf("query knowledge failed:%w",err)
	}
	defer rows.Close()

	var entries []KnowledgeEntry
	for rows.Next(){
       var entry KnowledgeEntry
	   if err:=ks.scanKnowledgeRow(rows,&entry);err!=nil{
		return 0,fmt.Errorf("scan row failed:%w",err)
	   }
	   entries=append(entries, entry)
	}
	if len(entries)==0{
		return 0,nil
	}

	texts:=make([]string,len(entries))
	for i,entry:=range entries{
		texts[i]=entry.Title+"\n"+entry.Content+"\n"+entry.Question
	}


	//向量化并存入embedding表
	batchSize:=100
	var indexed int

	for i:=0;i<len(texts);i+=batchSize{
		end:=i+batchSize

		if end>len(texts){
			end=len(texts)
		}

		batch:=texts[i:end]
		vectors,err:=ks.embeddingProvider.EmbedBatch(batch)
		if err!=nil{
			return indexed,fmt.Errorf("embed batch failed:%w",err)
		}

		tx,err:=ks.db.Begin()
		if err!=nil{
			return indexed,fmt.Errorf("begin transaction failed:%w",err)
		}

		stmt,err:=tx.Prepare(`
			INSERT OR REPLACE INTO embeddings (knowledge_id,vector,model,created_at)
			VALUES(?,?,?,strftime('%s','now'),)
		`)
		if err!=nil{
			tx.Rollback()
			return indexed,fmt.Errorf("prepare statement failed:%w",err)
		}

		for j,vector:=range vectors{
			if _,err:=stmt.Exec(entries[i+j].ID,ks.serializeVector(vector),ks.embeddingProvider.Model());err!=nil{
				tx.Rollback()
				return indexed,fmt.Errorf("exec statement failed:%w",err)
			}
		}
		if err:=tx.Commit();err!=nil{
			return indexed,fmt.Errorf("commit transaction failed :%w",err)
		}

		indexed+=len(vectors)

	}
	return indexed,nil

}

// 单条插入
func (ks *KnowledgeSearch) Insert(entry *KnowledgeEntry) error {
	tags := ""
	if len(entry.Tags) > 0 {
		tags = strings.Join(entry.Tags, ",")
	}

	_, err := ks.db.Exec(
		` 
		  INSERT OR REPLACE INTO knowledge (id,title,dimension,content,source_file,question,expert_answer,novice_answer,tags)
		  VALUES (?,?,?,?,?,?,?,?,?)
		`, entry.ID, entry.Title, entry.Dimension, entry.Content, entry.SourceFile,
		entry.Question, entry.ExpertAnswer, entry.NoviceAnswer, tags)

	return err
}

// 批量插入
func (ks *KnowledgeSearch) BulkInsert(entries []*KnowledgeEntry) error {
	tx, err := ks.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction failed:%w", err)
	}

	stmt, err := tx.Prepare(`
	    INSERT OR REPLACE INTO knowledge (id, title, dimension, content, source_file, question, expert_answer, novice_answer, tags)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare statement failed:%w", err)
	}

	for _, entry := range entries {
		tags := ""
		if len(entry.Tags) > 0 {
			tags = strings.Join(entry.Tags, ",")
		}

		if _, err := stmt.Exec(entry.ID, entry.Title, entry.Dimension, entry.Content, entry.SourceFile,
			entry.Question, entry.ExpertAnswer, entry.NoviceAnswer, tags); err != nil {
			tx.Rollback()
			return fmt.Errorf("exec statement failed:%w", err)
		}

	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction failed:%w", err)
	}
	return nil
}

func (ks *KnowledgeSearch) Count() (int, error) {
	var count int
	err := ks.db.QueryRow("SELECT COUNT(*)FROM knowledge").Scan(&count)
	return count, err
}

func (ks *KnowledgeSearch) cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dotProduct / denom
}

// 序列化，数据库认识字节流，不认识复杂数据结构，所以需要序列化将复杂数据结构变为字节流持久化到数据库
func (ks *KnowledgeSearch) serializeVector(vector []float64) []byte {
	buf := make([]byte, len(vector)*4)
	for i, v := range vector {
		binary.LittleEndian.PutUint32(buf[i*4:(i+1)*4], math.Float32bits(float32(v)))
	}
	return buf

}

// 反序列化
func (ks *KnowledgeSearch) deserializeVector(buf []byte) []float64 {
	vector := make([]float64, len(buf)/4)
	for i := 0; i < len(buf); i += 4 {
		vector[i/4] = float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[i : i+4])))
	}
	return vector
}

func (ks *KnowledgeSearch) mergeResults(fts, emb []*SearchResult, limit int) []*SearchResult {
	seen := make(map[string]bool)
	var merged []*SearchResult

	all := append(fts, emb...)
	for i := 0; i < len(all)-1; i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].Score > all[i].Score {
				all[i], all[j] = all[j], all[i]
			}
		}

	}

	for _, result := range all {
		if seen[result.Entry.ID] {
			continue
		}
		seen[result.Entry.ID] = true
		merged = append(merged, &SearchResult{
			Entry:     result.Entry,
			Score:     result.Score,
			MatchType: MatchTypeHybrid,
		})

		if len(merged) >= limit {
			break
		}

	}

	return merged
}

func (ks *KnowledgeSearch) scanKnowledgeRow(rows *sql.Rows, entry *KnowledgeEntry) error {
	var tags string
	err := rows.Scan(&entry.ID, &entry.Title, &entry.Dimension, &entry.Content,
		&entry.SourceFile, &entry.Question, &entry.ExpertAnswer, &entry.NoviceAnswer, &tags) //读取当前行的数据，填充到entry中

	if err != nil {
		return err
	}
	if tags != "" {
		entry.Tags = strings.Split(tags, ",")
	}

	return nil
}
